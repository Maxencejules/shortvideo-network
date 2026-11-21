"""Read the requirements-to-tests matrix and count its dispositions.

This is a document index. A count is not a passing test and not a release.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

FENCE = "```requirements-matrix\n"
HEADER = "id\tscope\tpages\trole\tgoals\tdisposition\tsummary"


def parse_matrix(text: str) -> list[dict[str, str]]:
    if FENCE not in text:
        raise ValueError("matrix fence missing")
    body = text.split(FENCE, 1)[1]
    if "\n```" not in body:
        raise ValueError("matrix fence did not close")
    body = body.split("\n```", 1)[0]
    lines = [line for line in body.splitlines() if line.strip()]
    if not lines or lines[0] != HEADER:
        raise ValueError("matrix header mismatch")
    rows: list[dict[str, str]] = []
    seen: set[str] = set()
    for line in lines[1:]:
        fields = line.split("\t")
        if len(fields) != 7:
            raise ValueError(f"matrix row has {len(fields)} fields")
        row = dict(zip(HEADER.split("\t"), fields, strict=True))
        if row["id"] in seen:
            raise ValueError(f"duplicate id {row['id']}")
        seen.add(row["id"])
        rows.append(row)
    return rows


def goal_set(raw: str) -> set[str]:
    if raw == "none":
        return set()
    return {part for part in raw.split(",") if part}


def summarize(rows: list[dict[str, str]]) -> str:
    in_scope = [row for row in rows if row["scope"] == "IN_SCOPE"]
    test = [row for row in in_scope if row["disposition"].startswith("TEST:")]
    blocker = [row for row in in_scope if row["disposition"].startswith("BLOCKER:")]
    out = [row for row in rows if row["scope"] != "IN_SCOPE"]
    goal30 = [row for row in rows if "30" in goal_set(row["goals"])]
    return (
        f"RESULT rows={len(rows)} in_scope={len(in_scope)} test={len(test)} "
        f"blocker={len(blocker)} out={len(out)} goal30={len(goal30)}"
    )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="audit-matrix")
    parser.add_argument("--requirements", type=Path, default=Path("docs/requirements.md"))
    parser.add_argument("--tsv", type=Path)
    args = parser.parse_args(argv)
    rows = parse_matrix(args.requirements.read_text(encoding="utf-8"))
    print(summarize(rows))
    if args.tsv is not None:
        lines = ["\t".join(HEADER.split("\t") + ["kind", "goal30"])]
        for row in rows:
            if row["scope"] != "IN_SCOPE":
                kind = "OUT"
            elif row["disposition"].startswith("TEST:"):
                kind = "TEST"
            elif row["disposition"].startswith("BLOCKER:"):
                kind = "BLOCKER"
            else:
                kind = "OTHER"
            goal30 = "yes" if "30" in goal_set(row["goals"]) else "no"
            fields = [row[column] for column in HEADER.split("\t")]
            fields.extend([kind, goal30])
            lines.append("\t".join(fields))
        args.tsv.parent.mkdir(parents=True, exist_ok=True)
        args.tsv.write_text("\n".join(lines) + "\n", encoding="utf-8")
        print(f"TSV {args.tsv}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
