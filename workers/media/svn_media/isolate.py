"""Run a command with rlimits and an unprivileged network namespace.

Arguments are a list. The shell is not used. The child environment is PATH,
LC_ALL, LANG, and TZ. A failure here does not fall back to an unrestricted process.
"""

from __future__ import annotations

import os
import signal
import subprocess
from dataclasses import dataclass
from pathlib import Path

from svn_media.limits import Failure

_STDOUT_CAP = 1_000_000
_CHILD_ENV = {
    "PATH": "/usr/bin:/bin",
    "LC_ALL": "C.UTF-8",
    "LANG": "C.UTF-8",
    "TZ": "UTC",
}


@dataclass(frozen=True)
class Bounds:
    wall_seconds: float
    cpu_seconds: int
    memory_bytes: int
    file_bytes: int
    network_namespace: bool = True


@dataclass(frozen=True)
class RunResult:
    returncode: int
    stdout: bytes
    stdout_truncated: bool


def run(argv: list[str], *, cwd: Path, bounds: Bounds) -> RunResult:
    """Run `argv` inside the disposable workspace `cwd`."""
    if not argv or any("\x00" in arg or not isinstance(arg, str) for arg in argv):
        raise Failure("INTERNAL")
    if bounds.wall_seconds <= 0 or bounds.file_bytes < 1:
        raise Failure("OVER_BUDGET")
    command = list(argv)
    if bounds.network_namespace:
        # uid 0 inside this user namespace is the caller's host uid, not host root.
        command = ["unshare", "--user", "--map-root-user", "--net", "--", *command]
    command = [
        "prlimit",
        f"--cpu={bounds.cpu_seconds}",
        f"--as={bounds.memory_bytes}",
        f"--fsize={bounds.file_bytes}",
        "--nofile=64",
        "--",
        *command,
    ]
    try:
        proc = subprocess.Popen(
            command,
            cwd=cwd,
            env=_CHILD_ENV,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            start_new_session=True,
        )
    except OSError:
        raise Failure("INTERNAL") from None
    try:
        out, _err = proc.communicate(timeout=bounds.wall_seconds)
    except subprocess.TimeoutExpired:
        _kill(proc)
        raise Failure("OVER_BUDGET") from None
    raw = out or b""
    truncated = len(raw) > _STDOUT_CAP
    if truncated:
        raw = raw[:_STDOUT_CAP]
    code = proc.returncode if proc.returncode is not None else 1
    if code < 0 and -code in (signal.SIGXFSZ, signal.SIGXCPU):
        raise Failure("OUTPUT_GROWTH" if -code == signal.SIGXFSZ else "OVER_BUDGET")
    if code == 128 + signal.SIGXFSZ:
        raise Failure("OUTPUT_GROWTH")
    if code == 128 + signal.SIGXCPU:
        raise Failure("OVER_BUDGET")
    return RunResult(code, raw, truncated)


def _kill(proc: subprocess.Popen[bytes]) -> None:
    try:
        os.killpg(proc.pid, signal.SIGKILL)
    except OSError:
        proc.kill()
    try:
        proc.communicate(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait(timeout=5)
