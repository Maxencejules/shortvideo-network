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


def run_node() -> int:
    completed = subprocess.run(
        ["npm", "test", "--workspace", "apps/web"],
        cwd=ROOT,
        check=False,
        text=True,
        capture_output=True,
    )
    sys.stdout.write(completed.stdout)
    sys.stderr.write(completed.stderr)
    output = completed.stdout + completed.stderr
    if completed.returncode != 0:
        return completed.returncode
    counts = re.findall(r"^# tests (\d+)$", output, re.MULTILINE)
    if not counts or int(counts[-1]) < 1:
        print("node test reported no tests; an empty suite is not verification")
        return 1
    if re.search(r"^# fail 0$", output, re.MULTILINE) is None:
        print("node test did not report zero failures")
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
