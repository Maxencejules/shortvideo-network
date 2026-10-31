"""Goal 04 environment commands. These tests do not start PostgreSQL or RabbitMQ."""

from __future__ import annotations

import json
import os
import shutil
import socket
import stat
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ENV = ROOT / "scripts" / "env"

PINS = {
    "rabbitmq-server-generic-unix-4.4.0.tar.xz": (
        "68c22d5dafa15c0911463aeca73c362d638cd6fb8e407ad39512959c6cc37f0f"
    ),
    "celery-5.6.3.tar.gz": ("177006bd2054b882e9f01be59abd8529e88879ef50d7918a7050c5a9f4e12912"),
    "ffmpeg_7:6.1.1-3ubuntu5_amd64.deb": (
        "1a23baace5f2688a47e28119aedae6993cd638e6279a7227dfc52d9a337a1c17"
    ),
    "amqp-tools_0.11.0-1ubuntu0.2_amd64.deb": (
        "0915bbfc3998bec2986d254555c3eb644eb96fc814d710cdb8314e5be49093f0"
    ),
}


def _run(args: list[str], env: dict[str, str]) -> subprocess.CompletedProcess[str]:
    merged = os.environ.copy()
    merged.update(env)
    return subprocess.run(
        args,
        cwd=ROOT,
        env=merged,
        text=True,
        capture_output=True,
        check=False,
    )


def test_checksum_pins_match_the_verified_artifacts() -> None:
    text = (ROOT / "deployments" / "pins" / "checksums.txt").read_text(encoding="utf-8")
    for name, digest in PINS.items():
        assert f"{digest}  {name}" in text
    install = (ENV / "install.sh").read_text(encoding="utf-8")
    assert "18.6-1.pgdg24.04+2" in install
    assert "1:27.3.4.18-1rmq1ppa1~ubuntu24.04.1" in install
    assert "7:6.1.1-3ubuntu5" in install
    assert "0.11.0-1ubuntu0.2" in install
    assert "3.12.3-1ubuntu0.17" in install
    assert "pip install celery" not in install.lower()


def test_environment_json_points_at_the_scripts() -> None:
    document = json.loads((ROOT / ".cursor" / "environment.json").read_text(encoding="utf-8"))
    assert document["user"] == "ubuntu"
    assert document["install"] == "bash scripts/env/install.sh"
    assert document["start"] == "bash scripts/env/start.sh"
    assert "password" not in json.dumps(document).lower()


def test_compose_file_pins_loopback_images() -> None:
    text = (ROOT / "deployments" / "compose" / "docker-compose.yml").read_text(encoding="utf-8")
    assert "sha256:f4b419e350fd98d4a8429603295208976dea4c8fb8326d818d346d3e29f1db6f" in text
    assert "sha256:9f48a2c0a501c5a7cf520d69eb3369ba85e614a195dde071f3dfa20077e54d26" in text
    assert "127.0.0.1:5432:5432" in text
    assert "127.0.0.1:5672:5672" in text
    assert "guest" not in text.lower()
    assert (ENV / "compose.sh").read_text(encoding="utf-8").find("BLOCKED") > 0


def test_local_workspace_is_gitignored() -> None:
    completed = subprocess.run(
        ["git", "check-ignore", "-q", "--", ".local/workspaces/dev/credentials.env"],
        cwd=ROOT,
        check=False,
    )
    assert completed.returncode == 0
    key = subprocess.run(
        ["git", "check-ignore", "-q", "--", ".local/workspaces/dev/devnet/validator.key"],
        cwd=ROOT,
        check=False,
    )
    assert key.returncode == 0


def test_reset_requires_the_workspace_name(tmp_path: Path) -> None:
    root = tmp_path / "workspaces"
    workspace = root / "disposable"
    workspace.mkdir(parents=True)
    (workspace / "marker").write_text("keep", encoding="utf-8")
    env = {"SVN_STATE_ROOT": str(root)}
    refused = _run(["bash", "scripts/env/reset.sh", "--workspace", "disposable"], env)
    assert refused.returncode != 0
    assert "refusing" in refused.stderr.lower()
    assert (workspace / "marker").is_file()
    mismatch = _run(
        [
            "bash",
            "scripts/env/reset.sh",
            "--workspace",
            "disposable",
            "--confirm",
            "other",
        ],
        env,
    )
    assert mismatch.returncode != 0
    assert (workspace / "marker").is_file()
    unmarked = _run(
        [
            "bash",
            "scripts/env/reset.sh",
            "--workspace",
            "disposable",
            "--confirm",
            "disposable",
        ],
        env,
    )
    assert unmarked.returncode != 0
    assert "missing .svn-workspace" in unmarked.stderr
    assert (workspace / "marker").is_file()
    (workspace / ".svn-workspace").write_text("svn-dev-workspace\n", encoding="utf-8")
    escaped = _run(
        ["bash", "scripts/env/reset.sh", "--workspace", "..", "--confirm", ".."],
        env,
    )
    assert escaped.returncode != 0
    assert (workspace / "marker").is_file()
    accepted = _run(
        [
            "bash",
            "scripts/env/reset.sh",
            "--workspace",
            "disposable",
            "--confirm",
            "disposable",
        ],
        env,
    )
    assert accepted.returncode == 0, accepted.stderr
    assert not workspace.exists()


def test_devnet_keys_are_fresh_and_not_replaced(tmp_path: Path) -> None:
    root = tmp_path / "workspaces"
    env = {"SVN_STATE_ROOT": str(root)}
    cloned = _run(
        ["bash", "scripts/env/devnet-keys.sh", "--workspace", "keys", "--from", "other"],
        env,
    )
    assert cloned.returncode != 0
    assert "clone" in cloned.stderr.lower()
    assert not (root / "keys" / "devnet" / "validator.key").exists()
    created = _run(["bash", "scripts/env/devnet-keys.sh", "--workspace", "keys"], env)
    assert created.returncode == 0, created.stderr
    key = root / "keys" / "devnet" / "validator.key"
    public = root / "keys" / "devnet" / "validator.pub"
    assert key.is_file()
    assert public.is_file()
    assert stat.S_IMODE(key.stat().st_mode) == 0o600
    body = key.read_bytes()
    again = _run(["bash", "scripts/env/devnet-keys.sh", "--workspace", "keys"], env)
    assert again.returncode != 0
    assert "refusing" in again.stderr.lower()
    assert key.read_bytes() == body
    other = _run(["bash", "scripts/env/devnet-keys.sh", "--workspace", "other"], env)
    assert other.returncode == 0, other.stderr
    assert (root / "other" / "devnet" / "validator.key").read_bytes() != body


def test_busy_port_and_missing_credentials_fail(tmp_path: Path) -> None:
    sock = socket.socket()
    sock.bind(("127.0.0.1", 0))
    sock.listen(1)
    port = sock.getsockname()[1]
    try:
        refused = _run(["bash", "scripts/env/require-port.sh", "127.0.0.1", str(port)], {})
    finally:
        sock.close()
    assert refused.returncode != 0
    assert "not available" in refused.stderr
    missing = _run(
        ["bash", "scripts/env/require-credentials.sh", str(tmp_path / "no-such.env")],
        {},
    )
    assert missing.returncode != 0
    assert "missing" in missing.stderr
    assert "refusing" in missing.stderr.lower()


def test_devnet_keys_require_a_workspace_name(tmp_path: Path) -> None:
    env = {"SVN_STATE_ROOT": str(tmp_path / "workspaces")}
    missing = _run(["bash", "scripts/env/devnet-keys.sh", "--workspace"], env)
    assert missing.returncode != 0
    assert "requires a workspace name" in missing.stderr


def test_reset_deletes_a_keys_only_workspace(tmp_path: Path) -> None:
    root = tmp_path / "workspaces"
    env = {"SVN_STATE_ROOT": str(root)}
    created = _run(["bash", "scripts/env/devnet-keys.sh", "--workspace", "keys"], env)
    assert created.returncode == 0, created.stderr
    removed = _run(
        ["bash", "scripts/env/reset.sh", "--workspace", "keys", "--confirm", "keys"],
        env,
    )
    assert removed.returncode == 0, removed.stderr
    assert "keys-only" in removed.stdout
    assert not (root / "keys").exists()
    mixed = root / "mixed"
    (mixed / "devnet").mkdir(parents=True)
    (mixed / "devnet" / "validator.key").write_text("not-a-real-key", encoding="utf-8")
    (mixed / "devnet" / "validator.pub").write_text("public", encoding="utf-8")
    (mixed / "notes").write_text("keep", encoding="utf-8")
    refused = _run(
        ["bash", "scripts/env/reset.sh", "--workspace", "mixed", "--confirm", "mixed"],
        env,
    )
    assert refused.returncode != 0
    assert (mixed / "notes").is_file()


def test_diagnose_redacts_docker_host() -> None:
    secret = "svn-docker-secret-token"
    completed = _run(
        ["bash", "scripts/env/diagnose.sh"],
        {"DOCKER_HOST": f"tcp://user:{secret}@127.0.0.1:2375"},
    )
    assert completed.returncode == 0, completed.stderr
    combined = completed.stdout + completed.stderr
    assert secret not in combined
    assert "DOCKER_HOST=set" in completed.stdout
    assert "DOCKER_HOST_TARGET=tcp://127.0.0.1:2375" in completed.stdout


def test_diagnose_reports_without_requiring_services() -> None:
    completed = _run(["bash", "scripts/env/diagnose.sh"], {})
    assert completed.returncode == 0, completed.stderr
    assert "nproc" in completed.stdout
    assert "Linux" in completed.stdout


def test_compose_is_blocked_when_docker_is_absent(tmp_path: Path) -> None:
    bindir = tmp_path / "bin"
    bindir.mkdir()
    dirname = shutil.which("dirname")
    assert dirname is not None
    os.symlink(dirname, bindir / "dirname")
    completed = subprocess.run(
        ["/bin/bash", "scripts/env/compose.sh"],
        cwd=ROOT,
        env={"PATH": str(bindir), "HOME": str(tmp_path)},
        text=True,
        capture_output=True,
        check=False,
    )
    assert completed.returncode == 2, completed.stderr
    assert "BLOCKED" in completed.stderr


def test_credentials_parser_does_not_execute_shell(tmp_path: Path) -> None:
    side = tmp_path / "pwned"
    cred = tmp_path / "credentials.env"
    cred.write_text(
        "\n".join(
            [
                "SVN_DB_USER=svn_db",
                f"SVN_DB_PASSWORD=$(touch {side})",
                "SVN_DB_NAME=svn_dev",
                "SVN_QUEUE_USER=svn_queue",
                "SVN_QUEUE_PASSWORD=abc",
                "SVN_POSTGRES_PORT=5432",
                "SVN_RABBITMQ_PORT=5672",
                "",
            ]
        ),
        encoding="utf-8",
    )
    completed = _run(["bash", "scripts/env/require-credentials.sh", str(cred)], {})
    assert completed.returncode == 0, completed.stderr
    assert not side.exists()
