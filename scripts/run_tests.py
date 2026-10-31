#!/usr/bin/env python3
"""Run unit tests. An empty suite is a failure, not verification."""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def python_bin() -> str:
    venv = ROOT / ".venv" / "bin" / "python"
    if venv.is_file():
        return str(venv)
    return sys.executable


def run(command: list[str], cwd: Path | None = None) -> subprocess.CompletedProcess[str]:
    print("+", " ".join(command), flush=True)
    return subprocess.run(
        command,
        cwd=cwd or ROOT,
        check=False,
        text=True,
    )


def run_python() -> int:
    completed = subprocess.run(
        [python_bin(), "-m", "pytest"],
        cwd=ROOT,
        check=False,
        text=True,
        capture_output=True,
    )
    sys.stdout.write(completed.stdout)
    sys.stderr.write(completed.stderr)
    if completed.returncode != 0:
        return completed.returncode
    if re.search(r"\b[1-9]\d* passed\b", completed.stdout) is None:
        print("pytest reported no passing tests; an empty suite is not verification")
        return 1
    return 0


NODE_FILE_SUFFIXES = (".mjs", ".cjs", ".js", ".mts", ".cts", ".ts")
NODE_TEST_GLOBS = (
    "**/*.test.mjs",
    "**/*.test.cjs",
    "**/*.test.js",
    "**/*.test.mts",
    "**/*.test.cts",
    "**/*.test.ts",
)


def node_test_files(root: Path | None = None) -> list[Path]:
    """Discover Node tests under apps/web, including nested directories."""
    base = root if root is not None else ROOT / "apps" / "web"
    found: set[Path] = set()
    for pattern in NODE_TEST_GLOBS:
        for path in base.glob(pattern):
            if path.is_file() and "node_modules" not in path.parts:
                found.add(path)
    return sorted(found)


def _is_filename_subtest(name: str) -> bool:
    base = Path(name).name
    return name.endswith(NODE_FILE_SUFFIXES) or base.endswith(NODE_FILE_SUFFIXES)


_SUBTEST = re.compile(r"^[ \t]*# Subtest: (.+)$", re.MULTILINE)


def real_node_subtests(tap: str) -> list[str]:
    """Executed test() results. Suites, skips, todos, and file placeholders do not count."""
    real: list[str] = []
    for match in _SUBTEST.finditer(tap):
        name = match.group(1).strip()
        if _is_filename_subtest(name):
            continue
        ok_line = re.compile(
            r"^[ \t]*(?:not )?ok \d+ - "
            + re.escape(name)
            + r"(?: # (?P<flag>SKIP|TODO)\b.*)?[ \t]*$",
            re.MULTILINE,
        )
        ok = ok_line.search(tap, match.end())
        if ok is None or ok.group("flag") in {"SKIP", "TODO"}:
            continue
        window = tap[ok.end() :]
        nxt = _SUBTEST.search(window)
        if nxt is not None:
            window = window[: nxt.start()]
        kind = re.search(r"type:\s*['\"](\w+)['\"]", window)
        if kind is not None and kind.group(1) == "suite":
            continue
        real.append(name)
    return real


def node_run_status(output: str, returncode: int) -> int:
    """1 when the file has no executed test, or the process did not pass."""
    if not real_node_subtests(output):
        return 1
    if returncode != 0:
        return returncode
    if re.search(r"^# fail 0$", output, re.MULTILINE) is None:
        return 1
    if re.search(r"^# pass [1-9]", output, re.MULTILINE) is None:
        return 1
    return 0


def node_command(path: Path) -> list[str]:
    command = ["node"]
    if path.suffix in {".ts", ".mts", ".cts"}:
        command.append("--experimental-strip-types")
    command.extend(["--test", "--test-reporter", "tap", str(path)])
    return command


def run_node() -> int:
    files = node_test_files()
    if not files:
        print("node test files missing; an empty suite is not verification")
        return 1
    for path in files:
        completed = subprocess.run(
            node_command(path),
            cwd=ROOT,
            check=False,
            text=True,
            capture_output=True,
        )
        sys.stdout.write(completed.stdout)
        sys.stderr.write(completed.stderr)
        output = completed.stdout + completed.stderr
        status = node_run_status(output, completed.returncode)
        if status != 0:
            print(f"{path.relative_to(ROOT)}: node tests are not verification ({status})")
            return status
    return 0


GO_MODULES = ("chain", "proto", "agents/provider")


def run_go() -> int:
    saw_ok = False
    for module in GO_MODULES:
        completed = subprocess.run(
            ["go", "test", "./..."],
            cwd=ROOT / module,
            check=False,
            text=True,
            capture_output=True,
        )
        sys.stdout.write(completed.stdout)
        sys.stderr.write(completed.stderr)
        output = completed.stdout + completed.stderr
        if completed.returncode != 0:
            print(f"{module} tests failed: {completed.returncode}")
            return completed.returncode
        if re.search(r"^ok\s+\S+", output, re.MULTILINE):
            saw_ok = True
        else:
            print(f"{module}: empty go test suite is not verification")
            return 1
    if not saw_ok:
        print("go test reported no passing packages; an empty suite is not verification")
        return 1
    return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--only", choices=("all", "python", "node", "go"), default="all")
    args = parser.parse_args()
    selected = {
        "python": run_python,
        "node": run_node,
        "go": run_go,
    }
    names = list(selected) if args.only == "all" else [args.only]
    for name in names:
        code = selected[name]()
        if code != 0:
            print(f"{name} tests failed: {code}")
            return code
    return 0


if __name__ == "__main__":
    sys.exit(main())
