"""Resolve each requirements-matrix TEST row to a test function and a log result.

A ``TEST:`` sentence assigns a check. It is not by itself a test that exists or
a test that ran. This script resolves a row to a file and a function, then
looks for that function in the stamped logs. Unresolved and unrun rows are
listed. They are not passes. A row count is not a release.
"""

from __future__ import annotations

import argparse
import re
import sys
from collections.abc import Iterable
from dataclasses import dataclass, field
from pathlib import Path

FENCE = "```requirements-matrix\n"
HEADER = "id\tscope\tpages\trole\tgoals\tdisposition\tsummary"
_MODULE_PREFIX = "github.com/Maxencejules/shortvideo-network/"
_SKIP_DIRS = {".git", "node_modules", ".venv", ".toolchains", "dist", "coverage"}
_REQ = re.compile(r"REQ-[A-Z]+-\d+")
_GO_FUNC = re.compile(r"^func (?:\([^)]*\) )?(Test[A-Z][A-Za-z0-9_]*)\(")
_PY_FUNC = re.compile(r"^(?:async )?def (test_[A-Za-z0-9_]+)\(")
_TS_FUNC = re.compile(r"""^(?:test|it)\(\s*(['"`])(.+?)\1""")
_GO_NAME = re.compile(r"\b(Test[A-Z][A-Za-z0-9_]*)\b")
_PY_NAME = re.compile(r"\b(test_[A-Za-z0-9_]+)\b")
_PATH = re.compile(r"(?:[A-Za-z0-9_.-]+/)+[A-Za-z0-9_.-]+\.(?:go|py|ts|tsx|js)\b")
_INDEX = re.compile(r"^(PASS|FAIL|SKIP) (\S+) (\S+)$")
_VERBOSE = re.compile(r"^--- (PASS|FAIL|SKIP): (\S+)")
_PYTEST = re.compile(r"^(\S+\.py)::([^\s]+) (PASSED|FAILED|SKIPPED|ERROR)\b")
_TAP = re.compile(r"^(ok|not ok) \d+ - (.+)$")
_RANK = {"fail": 3, "pass": 2, "skip": 1}


@dataclass(frozen=True)
class Target:
    path: str
    line: int
    function: str
    directory: str


@dataclass
class TestIndex:
    root: Path
    targets: list[Target] = field(default_factory=list)
    by_req: dict[str, list[Target]] = field(default_factory=dict)
    by_name: dict[str, list[Target]] = field(default_factory=dict)
    files: set[str] = field(default_factory=set)

    def add(self, target: Target, reqs: Iterable[str]) -> None:
        self.targets.append(target)
        self.files.add(target.path)
        self.by_name.setdefault(target.function, []).append(target)
        for req in reqs:
            bucket = self.by_req.setdefault(req, [])
            if target not in bucket:
                bucket.append(target)


@dataclass
class Observations:
    qualified: dict[tuple[str, str], set[str]] = field(default_factory=dict)
    by_name: dict[str, set[str]] = field(default_factory=dict)
    heads: list[str] = field(default_factory=list)

    def add(self, status: str, function: str, qualifier: str = "") -> None:
        base = function.split("/", 1)[0].split("[", 1)[0]
        self.by_name.setdefault(base, set()).add(status)
        if qualifier:
            self.qualified.setdefault((qualifier, base), set()).add(status)
            self.qualified.setdefault((qualifier, function), set()).add(status)


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


def _reqs(text: str) -> set[str]:
    return set(_REQ.findall(text))


def _py_docstring(lines: list[str], signature: int) -> list[str]:
    """The string that is the first statement of a function. Other body text is not a citation."""
    index = signature + 1
    while index < len(lines) and lines[index].strip() == "":
        index += 1
    if index >= len(lines):
        return []
    stripped = lines[index].lstrip()
    quote = ""
    for marker in ('"""', "'''"):
        if stripped.startswith(marker):
            quote = marker
            break
    if not quote:
        return []
    block = [lines[index]]
    if stripped.count(quote) >= 2 and len(stripped) > 3:
        return block
    index += 1
    while index < len(lines):
        block.append(lines[index])
        if quote in lines[index]:
            break
        index += 1
    return block


def _header_text(lines: list[str], header_end: int, kind: str) -> str:
    """Comments and a module docstring. Code before the first test is not a citation."""
    chunk = lines[:header_end]
    if kind != "py":
        return "\n".join(line for line in chunk if _is_comment(line, kind))
    parts = [line for line in chunk if _is_comment(line, "py")]
    index = 0
    while index < len(chunk):
        stripped = chunk[index].lstrip()
        if stripped != "" and not stripped.startswith("#") and not chunk[index].startswith("#!"):
            break
        index += 1
    if index < len(chunk):
        parts.extend(_py_docstring(chunk, index - 1))
    return "\n".join(parts)


def _unique(items: Iterable[str]) -> list[str]:
    seen: set[str] = set()
    out: list[str] = []
    for item in items:
        if item not in seen:
            seen.add(item)
            out.append(item)
    return out


def _comment_start(lines: list[str], func_at: int, kind: str) -> int:
    index = func_at - 1
    while index >= 0 and lines[index].strip() == "":
        index -= 1
    while index >= 0 and _is_comment(lines[index], kind):
        index -= 1
    return index + 1


def _is_comment(line: str, kind: str) -> bool:
    stripped = line.lstrip()
    if kind == "py":
        return stripped.startswith("#")
    return stripped.startswith("//")


def _scan_text(rel: str, text: str, kind: str) -> list[tuple[Target, set[str]]]:
    lines = text.splitlines()
    found: list[tuple[int, str]] = []
    for number, line in enumerate(lines):
        if kind == "go":
            match = _GO_FUNC.match(line)
            name = match.group(1) if match else ""
        elif kind == "py":
            match = _PY_FUNC.match(line)
            name = match.group(1) if match else ""
        else:
            match = _TS_FUNC.match(line)
            name = match.group(2) if match else ""
        if name:
            found.append((number, name))
    if not found:
        return []
    header_end = _comment_start(lines, found[0][0], kind)
    header_reqs = _reqs(_header_text(lines, header_end, kind))
    directory = str(Path(rel).parent.as_posix())
    if directory == ".":
        directory = ""
    scanned: list[tuple[Target, set[str]]] = []
    for start, name in found:
        comment_at = _comment_start(lines, start, kind)
        cited = lines[comment_at : start + 1]
        if kind == "py":
            cited.extend(_py_docstring(lines, start))
        own = _reqs("\n".join(cited))
        target = Target(rel, start + 1, name, directory)
        scanned.append((target, header_reqs | own))
    return scanned


def index_tests(root: Path) -> TestIndex:
    index = TestIndex(root=root)
    for path in sorted(root.rglob("*")):
        if not path.is_file():
            continue
        if any(part in _SKIP_DIRS for part in path.relative_to(root).parts):
            continue
        name = path.name
        if name.endswith("_test.go"):
            kind = "go"
        elif (name.startswith("test_") and name.endswith(".py")) or name.endswith("_test.py"):
            kind = "py"
        elif name.endswith((".test.ts", ".test.tsx", ".test.js")):
            kind = "ts"
        else:
            continue
        rel = path.relative_to(root).as_posix()
        text = path.read_text(encoding="utf-8", errors="replace")
        for target, reqs in _scan_text(rel, text, kind):
            index.add(target, reqs)
    return index


def named_in(disposition: str) -> tuple[list[str], list[str]]:
    paths = _unique(_PATH.findall(disposition))
    remainder = _PATH.sub(" ", disposition)
    names = _unique(_GO_NAME.findall(remainder) + _PY_NAME.findall(remainder))
    return names, paths


def resolve_row(
    row_id: str, disposition: str, index: TestIndex
) -> tuple[tuple[Target, ...], tuple[str, ...]]:
    names, paths = named_in(disposition)
    problems: list[str] = []
    chosen: list[Target] = []
    if names or paths:
        for rel in paths:
            if not (index.root / rel).is_file():
                problems.append(f"missing path {rel}")
        for name in names:
            found = index.by_name.get(name, [])
            if paths:
                found = [item for item in found if item.path in paths]
            if found:
                chosen.extend(found)
            elif name not in index.by_name:
                problems.append(f"missing function {name}")
            else:
                problems.append(f"function {name} is not in a named file")
        if problems:
            return (), tuple(problems)
        if chosen:
            return tuple(_dedupe(chosen)), ()
        cited = [item for item in index.by_req.get(row_id, []) if not paths or item.path in paths]
        if cited:
            return tuple(_dedupe(cited)), ()
        return (), ("named file has no test function for this id",)
    cited = index.by_req.get(row_id, [])
    if cited:
        return tuple(_dedupe(cited)), ()
    return (), ("no test function",)


def _dedupe(targets: list[Target]) -> list[Target]:
    seen: set[tuple[str, int, str]] = set()
    out: list[Target] = []
    for target in targets:
        key = (target.path, target.line, target.function)
        if key not in seen:
            seen.add(key)
            out.append(target)
    return out


def _fold(statuses: set[str]) -> str:
    if not statuses:
        return "unrun"
    return max(statuses, key=lambda item: _RANK.get(item, 0))


def _remember(obs: Observations, word: str, function: str, qualifier: str = "") -> None:
    status = {"PASS": "pass", "FAIL": "fail", "SKIP": "skip"}.get(word)
    if status is None:
        return
    obs.add(status, function, qualifier)


def parse_observations(text: str) -> Observations:
    obs = Observations()
    for raw in text.splitlines():
        if raw.startswith("HEAD "):
            obs.heads.append(raw.split(" ", 1)[1].strip())
            continue
        indexed = _INDEX.match(raw)
        if indexed:
            package = indexed.group(2)
            directory = package.removeprefix(_MODULE_PREFIX)
            _remember(obs, indexed.group(1), indexed.group(3), directory)
            continue
        verbose = _VERBOSE.match(raw)
        if verbose:
            _remember(obs, verbose.group(1), verbose.group(2))
            continue
        pytest_line = _PYTEST.match(raw)
        if pytest_line:
            word = {"PASSED": "PASS", "FAILED": "FAIL", "SKIPPED": "SKIP", "ERROR": "FAIL"}[
                pytest_line.group(3)
            ]
            _remember(obs, word, pytest_line.group(2), pytest_line.group(1))
            continue
        tap = _TAP.match(raw)
        if tap:
            title = tap.group(2).strip()
            word = "FAIL" if tap.group(1) == "not ok" else "PASS"
            if " # SKIP" in title:
                word = "SKIP"
                title = title.split(" # SKIP", 1)[0].strip()
            _remember(obs, word, title)
    return obs


def load_logs(paths: list[Path], expect_head: str) -> tuple[Observations, list[str]]:
    merged = Observations()
    notes: list[str] = []
    if not paths:
        notes.append("LOG none")
        return merged, notes
    for path in paths:
        text = path.read_text(encoding="utf-8", errors="replace")
        parsed = parse_observations(text)
        head = parsed.heads[0] if parsed.heads else "absent"
        if expect_head and head != expect_head:
            notes.append(f"LOG {path} head={head} ignored")
            continue
        notes.append(f"LOG {path} head={head} accepted")
        merged.heads.extend(parsed.heads)
        for key, statuses in parsed.qualified.items():
            merged.qualified.setdefault(key, set()).update(statuses)
        for name, statuses in parsed.by_name.items():
            merged.by_name.setdefault(name, set()).update(statuses)
    return merged, notes


def status_of(target: Target, obs: Observations, index: TestIndex) -> str:
    found: set[str] = set()
    for key in ((target.directory, target.function), (target.path, target.function)):
        found.update(obs.qualified.get(key, set()))
    if not found and len(index.by_name.get(target.function, [])) == 1:
        found.update(obs.by_name.get(target.function, set()))
    return _fold(found)


def _where(target: Target) -> str:
    return f"{target.path}:{target.line} {target.function}"


def render(
    rows: list[dict[str, str]], index: TestIndex, obs: Observations, log_notes: list[str]
) -> str:
    counts = {
        "resolved": 0,
        "unresolved": 0,
        "pass": 0,
        "fail": 0,
        "skip": 0,
        "unrun": 0,
        "mixed": 0,
    }
    details: list[str] = []
    for row in rows:
        if row["scope"] != "IN_SCOPE":
            details.append(f"OUT {row['id']} {row['scope']}")
            continue
        if row["disposition"].startswith("BLOCKER:"):
            details.append(f"BLOCKER {row['id']}")
            continue
        if not row["disposition"].startswith("TEST:"):
            details.append(f"OTHER {row['id']}")
            continue
        targets, problems = resolve_row(row["id"], row["disposition"], index)
        if problems or not targets:
            counts["unresolved"] += 1
            reason = "; ".join(problems) if problems else "no test function"
            details.append(f"UNRESOLVED {row['id']} {reason}")
            continue
        counts["resolved"] += 1
        statuses = [status_of(target, obs, index) for target in targets]
        if "fail" in statuses:
            verdict = "fail"
        elif "unrun" in statuses:
            verdict = "unrun"
        elif "pass" in statuses and "skip" in statuses:
            verdict = "mixed"
        elif "skip" in statuses:
            verdict = "skip"
        else:
            verdict = "pass"
        counts[verdict] += 1
        if verdict == "pass":
            continue
        if verdict == "mixed":
            parts = [
                f"{_where(target)} {status}"
                for target, status in zip(targets, statuses, strict=True)
            ]
            details.append(f"MIXED {row['id']} " + "; ".join(parts))
            continue
        words = {"fail": "FAIL", "unrun": "UNRUN", "skip": "SKIP"}
        for target, status in zip(targets, statuses, strict=True):
            if status == "pass":
                continue
            details.append(f"{words[status]} {row['id']} {_where(target)}")
    base = summarize(rows)
    extra = (
        f" resolved={counts['resolved']} unresolved={counts['unresolved']}"
        f" pass={counts['pass']} fail={counts['fail']} skip={counts['skip']}"
        f" unrun={counts['unrun']} mixed={counts['mixed']}"
    )
    return "\n".join([base + extra, *log_notes, *details])


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="audit-matrix")
    parser.add_argument("--requirements", type=Path, default=Path("docs/requirements.md"))
    parser.add_argument("--root", type=Path, default=Path("."))
    parser.add_argument("--log", type=Path, action="append", default=[])
    parser.add_argument("--expect-head", default="")
    parser.add_argument("--tsv", type=Path)
    args = parser.parse_args(argv)
    rows = parse_matrix(args.requirements.read_text(encoding="utf-8"))
    index = index_tests(args.root)
    obs, notes = load_logs(args.log, args.expect_head)
    print(render(rows, index, obs, notes))
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
