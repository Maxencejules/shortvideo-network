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


def node_test_files() -> list[Path]:
    directory = ROOT / "apps" / "web" / "test"
    files = [
        path for path in directory.glob("*") if path.is_file() and path.name.endswith(".test.mjs")
    ]
    return sorted(files)


def real_node_subtests(tap: str) -> list[str]:
    """Return test() names. A file with no tests is reported as one passing file."""
    names = re.findall(r"^# Subtest: (.+)$", tap, re.MULTILINE)
    real: list[str] = []
    for name in names:
        if name.endswith(NODE_FILE_SUFFIXES) or Path(name).name.endswith(NODE_FILE_SUFFIXES):
            continue
        real.append(name)
    return real


def run_node() -> int:
    files = node_test_files()
    if not files:
        print("node test files missing; an empty suite is not verification")
        return 1
    for path in files:
        completed = subprocess.run(
            ["node", "--test", "--test-reporter", "tap", str(path)],
            cwd=ROOT,
            check=False,
            text=True,
            capture_output=True,
        )
        sys.stdout.write(completed.stdout)
        sys.stderr.write(completed.stderr)
        output = completed.stdout + completed.stderr
        real = real_node_subtests(output)
        if completed.returncode != 0:
            print(f"{path.relative_to(ROOT)}: node tests failed")
            return completed.returncode
        if not real:
            print(f"{path.relative_to(ROOT)}: zero tests; an empty file is not verification")
            return 1
        if re.search(r"^# fail 0$", output, re.MULTILINE) is None:
            print(f"{path.relative_to(ROOT)}: node test did not report zero failures")
            return 1
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
