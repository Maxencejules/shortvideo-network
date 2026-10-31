#!/usr/bin/env python3
"""Reject GitHub Actions workflows that widen trust or unpin actions."""

from __future__ import annotations

import re
import sys
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]
WORKFLOWS = ROOT / ".github" / "workflows"
ACTION_PINS = {
    "actions/checkout": "11d5960a326750d5838078e36cf38b85af677262",
    "actions/setup-go": "924ae3a1cded613372ab5595356fb5720e22ba16",
    "actions/setup-node": "249970729cb0ef3589644e2896645e5dc5ba9c38",
    "actions/setup-python": "ece7cb06caefa5fff74198d8649806c4678c61a1",
}
SHA = re.compile(r"^[0-9a-f]{40}$")
MAX_TIMEOUT = 20


def check_text(path: Path, text: str) -> list[str]:
    errors: list[str] = []
    if "pull_request_target" in text:
        errors.append(
            f"{path.name}: pull_request_target checks out untrusted code with elevated credentials"
        )
    if re.search(r"\bsecrets\.", text):
        errors.append(f"{path.name}: secrets context")
    if re.search(r"\bgh\s+pr\s+merge\b", text) or "auto-merge" in text:
        errors.append(f"{path.name}: automatic merge")
    if re.search(r"\bproduction\b", text, re.IGNORECASE):
        errors.append(f"{path.name}: production target")
    return errors


def check_job(path: Path, name: str, job: object) -> list[str]:
    errors: list[str] = []
    if not isinstance(job, dict):
        return [f"{path.name}: job {name} is not a mapping"]
    permissions = job.get("permissions")
    if permissions is not None and permissions != {"contents": "read"}:
        errors.append(f"{path.name}: job {name} permissions {permissions}")
    timeout = job.get("timeout-minutes")
    if not isinstance(timeout, int) or timeout < 1 or timeout > MAX_TIMEOUT:
        errors.append(f"{path.name}: job {name} timeout {timeout}")
    environment = job.get("environment")
    if environment not in (None, ""):
        errors.append(f"{path.name}: job {name} environment {environment}")
    steps = job.get("steps")
    if not isinstance(steps, list) or not steps:
        errors.append(f"{path.name}: job {name} has no steps")
        return errors
    for step in steps:
        if not isinstance(step, dict):
            errors.append(f"{path.name}: job {name} step is not a mapping")
            continue
        uses = step.get("uses")
        if uses is None:
            continue
        if not isinstance(uses, str):
            errors.append(f"{path.name}: job {name} uses is not a string")
            continue
        if uses.startswith("./"):
            errors.append(f"{path.name}: local action {uses} is not used")
            continue
        if "@" not in uses:
            errors.append(f"{path.name}: unpinned {uses}")
            continue
        action, sha = uses.split("@", 1)
        if not SHA.fullmatch(sha):
            errors.append(f"{path.name}: {action} is not a full commit SHA")
            continue
        expected = ACTION_PINS.get(action)
        if expected is None:
            errors.append(f"{path.name}: unexpected action {action}")
        elif expected != sha:
            errors.append(f"{path.name}: {action} SHA is not the verified pin")
        raw_with = step.get("with")
        options: dict[str, object] = raw_with if isinstance(raw_with, dict) else {}
        if "token" in options or "ssh-key" in options:
            errors.append(f"{path.name}: {action} receives a credential input")
        if action == "actions/checkout" and options.get("persist-credentials") is not False:
            errors.append(f"{path.name}: checkout persists credentials")
        if action == "actions/setup-go" and options.get("cache") is not False:
            errors.append(f"{path.name}: setup-go cache is not disabled")
        if action == "actions/setup-node" and "cache" in options:
            errors.append(f"{path.name}: setup-node rejects a false cache value")
    return errors


def check_workflow(path: Path) -> list[str]:
    text = path.read_text(encoding="utf-8")
    errors = check_text(path, text)
    data = yaml.safe_load(text)
    if not isinstance(data, dict):
        return errors + [f"{path.name}: workflow is not a mapping"]
    if data.get("permissions") != {"contents": "read"}:
        errors.append(f"{path.name}: workflow permissions are not contents read")
    jobs = data.get("jobs")
    if not isinstance(jobs, dict) or not jobs:
        return errors + [f"{path.name}: no jobs"]
    for name, job in jobs.items():
        errors.extend(check_job(path, str(name), job))
    return errors


def main() -> int:
    paths = sorted(WORKFLOWS.glob("*.yml")) + sorted(WORKFLOWS.glob("*.yaml"))
    if not paths:
        print("no workflows")
        return 1
    errors: list[str] = []
    for path in paths:
        errors.extend(check_workflow(path))
    if errors:
        for error in errors:
            print(error)
        return 1
    print(f"workflow policy ok files {len(paths)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
