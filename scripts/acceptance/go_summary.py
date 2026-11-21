"""Turn `go test -json` lines into pass, fail, and skip counts.

Lines that are not JSON are counted and printed. They are not dropped.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any


def summarize(text: str) -> str:
    passed = 0
    failed = 0
    skipped = 0
    packages: list[str] = []
    skips: list[str] = []
    fails: list[str] = []
    nonjson = 0
    output: dict[str, list[str]] = {}
    for raw in text.splitlines():
        if not raw.strip():
            continue
        try:
            event: Any = json.loads(raw)
        except json.JSONDecodeError:
            nonjson += 1
            continue
        if not isinstance(event, dict):
            nonjson += 1
            continue
        action = event.get("Action")
        package = event.get("Package")
        test = event.get("Test")
        if not isinstance(action, str) or not isinstance(package, str):
            continue
        key = f"{package} {test}" if isinstance(test, str) else package
        if action == "output" and isinstance(event.get("Output"), str):
            output.setdefault(key, []).append(event["Output"])
            continue
        if action == "pass" and not isinstance(test, str):
            packages.append(f"ok {package}")
        elif action == "fail" and not isinstance(test, str):
            packages.append(f"FAIL {package}")
        elif action == "skip" and isinstance(test, str):
            skipped += 1
            skips.append(f"SKIP {package} {test}")
        elif action == "pass" and isinstance(test, str):
            passed += 1
        elif action == "fail" and isinstance(test, str):
            failed += 1
            fails.append(f"FAIL {package} {test}")
    lines = [
        f"RESULT passed={passed} failed={failed} skipped={skipped} "
        f"packages={len(packages)} nonjson={nonjson}"
    ]
    lines.extend(packages)
    lines.extend(skips)
    lines.extend(fails)
    for fail in fails:
        parts = fail.split(" ", 2)
        package, test = parts[1], parts[2]
        chunk = "".join(output.get(f"{package} {test}", []))
        if chunk:
            tail = "".join(chunk.splitlines(keepends=True)[-20:])
            lines.append(f"OUTPUT {package} {test}")
            lines.append(tail.rstrip("\n"))
    return "\n".join(lines)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="go-summary")
    parser.add_argument("path", type=Path)
    args = parser.parse_args(argv)
    text = summarize(args.path.read_text(encoding="utf-8", errors="replace"))
    print(text)
    first = text.splitlines()[0]
    failed = int(first.split("failed=", 1)[1].split(" ", 1)[0])
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
