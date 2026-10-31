#!/usr/bin/env python3
"""Write a CycloneDX inventory from the lockfiles.

Hashes stay in the lockfiles. This inventory does not invent licences.
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "docs" / "sbom.cdx.json"
PIP_NAME = re.compile(r"^([A-Za-z0-9_.-]+)==([^\\\s]+)")
GO_SUM = re.compile(r"^(\S+) (\S+) h1:")


def pip_components() -> list[dict[str, object]]:
    text = (ROOT / "requirements.lock").read_text(encoding="utf-8")
    components: list[dict[str, object]] = []
    for line in text.splitlines():
        match = PIP_NAME.match(line)
        if match is None:
            continue
        name, version = match.group(1), match.group(2)
        components.append(
            {
                "type": "library",
                "name": name,
                "version": version,
                "purl": f"pkg:pypi/{name.lower()}@{version}",
                "scope": "required",
            }
        )
    return components


def npm_components() -> list[dict[str, object]]:
    document = json.loads((ROOT / "package-lock.json").read_text(encoding="utf-8"))
    packages = document.get("packages", {})
    components: list[dict[str, object]] = []
    for path, meta in packages.items():
        if not path.startswith("node_modules/") or not isinstance(meta, dict):
            continue
        if "node_modules/" in path[len("node_modules/") :]:
            # Nested copies are still dependencies. Keep them.
            pass
        version = meta.get("version")
        if not isinstance(version, str):
            continue
        name = path.split("node_modules/")[-1]
        components.append(
            {
                "type": "library",
                "name": name,
                "version": version,
                "purl": f"pkg:npm/{name}@{version}",
                "scope": "required",
            }
        )
    return components


def go_components() -> list[dict[str, object]]:
    components: list[dict[str, object]] = []
    seen: set[str] = set()
    for path in sorted(ROOT.rglob("go.sum")):
        if ".toolchains" in path.parts or ".venv" in path.parts:
            continue
        for line in path.read_text(encoding="utf-8").splitlines():
            match = GO_SUM.match(line)
            if match is None:
                continue
            module, version = match.group(1), match.group(2)
            purl = f"pkg:golang/{module}@{version}"
            if purl in seen:
                continue
            seen.add(purl)
            components.append(
                {
                    "type": "library",
                    "name": module,
                    "version": version,
                    "purl": purl,
                    "scope": "required",
                }
            )
    return components


def bom() -> dict[str, object]:
    components = pip_components() + npm_components() + go_components()
    components.sort(key=lambda item: str(item["purl"]))
    return {
        "bomFormat": "CycloneDX",
        "specVersion": "1.6",
        "version": 1,
        "metadata": {
            "component": {
                "type": "application",
                "name": "shortvideo-network",
                "version": "0.0.0",
            },
            "properties": [
                {
                    "name": "svn:note",
                    "value": (
                        "Inventory of locked Python, npm, and Go modules. "
                        "No container image is built, so this file has no image digest. "
                        "Licences for direct dependencies are in docs/dependencies.md."
                    ),
                }
            ],
        },
        "components": components,
    }


def main() -> int:
    document = bom()
    components = document["components"]
    if not isinstance(components, list):
        raise RuntimeError("sbom components")
    text = json.dumps(document, indent=2, sort_keys=True) + "\n"
    if "--check" in sys.argv:
        current = OUT.read_text(encoding="utf-8") if OUT.is_file() else ""
        if current != text:
            print("sbom drift")
            return 1
        print(f"sbom ok components {len(components)}")
        return 0
    OUT.write_text(text, encoding="utf-8")
    print(f"wrote {OUT} components {len(components)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
