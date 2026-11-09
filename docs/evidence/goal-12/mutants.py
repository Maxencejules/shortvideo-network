#!/usr/bin/env python3
"""Goal 12 mutation runner (standard library only).

Exports a revision, checks that each mutant's search string is unique, runs
the named killer tests on the unmutated copy, then once per mutant. Exit 0
only when every mutant is KILLED. A build failure is not a kill.

Usage:
  python3 docs/evidence/goal-12/mutants.py [--rev REV] [--check] [--list]
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]

# id, file, old, new, package, tests
MUTANTS: list[tuple[str, str, str, str, str, tuple[str, ...]]] = [
    (
        "floor-zero",
        "services/indexer/internal/chain/chain.go",
        "const floorMinus int64 = 1",
        "const floorMinus int64 = 0",
        "services/indexer/internal/chain",
        ("TestAppFloor",),
    ),
    (
        "floor-two",
        "services/indexer/internal/chain/chain.go",
        "const floorMinus int64 = 1",
        "const floorMinus int64 = 2",
        "services/indexer/internal/chain",
        ("TestAppFloor",),
    ),
    (
        "chain-id-accepted",
        "services/indexer/internal/chain/chain.go",
        "if st.ChainID != c.chainID {",
        "if st.ChainID != st.ChainID {",
        "services/indexer/internal/chain",
        ("TestStatusChainID",),
    ),
    (
        "commit-hash-unchecked",
        "services/indexer/internal/chain/chain.go",
        "case !bytes.Equal(h.Hash, h.CommitHash):",
        "case bytes.Equal(h.Hash, h.Hash) && false:",
        "services/indexer/internal/chain",
        ("TestVerifyRejectsMismatch",),
    ),
    (
        "event-version-flipped",
        "services/indexer/internal/project/project.go",
        "if version != AcceptedVersion {",
        "if version == AcceptedVersion {",
        "services/indexer/internal/project",
        ("TestDecodeVersionAndFailure",),
    ),
    (
        "failed-tx-applied",
        "services/indexer/internal/project/project.go",
        "if tx.Code != 0 {",
        "if tx.Code == 0 {",
        "services/indexer/internal/project",
        ("TestDecodeVersionAndFailure",),
    ),
    (
        "gap-applied",
        "services/indexer/internal/store/store.go",
        "if block.Height != have+1 {",
        "if block.Height == have+1 {",
        "services/indexer/internal/store",
        ("TestApplyIsAtomicAndIdempotent",),
    ),
    (
        "renewal-invented",
        "services/indexer/internal/store/store.go",
        "if l.RenewOnly || l.EndOnly {",
        "if false && (l.RenewOnly || l.EndOnly) {",
        "services/indexer/internal/store",
        ("TestRenewUnknownLeaseDoesNotInventOne",),
    ),
    (
        "rebuild-allows-foreign-table",
        "services/indexer/internal/store/rebuild.go",
        "if !projectionTables[name] {",
        "if projectionTables[name] && false {",
        "services/indexer/internal/store",
        ("TestRebuildLeavesOtherSchemas",),
    ),
    (
        "runtime-can-delete",
        "services/indexer/internal/store/migrations/0001_indexer.pgsql",
        "GRANT SELECT ON indexer.chain_binding TO svn_indexer;",
        "GRANT SELECT, DELETE ON indexer.chain_binding TO svn_indexer;",
        "services/indexer/internal/store",
        ("TestRebuildLeavesOtherSchemas",),
    ),
    (
        "manifest-change-ignored",
        "services/indexer/internal/reconcile/reconcile.go",
        (
            "if !got.Withdrawn && (got.Manifest != row.Manifest || "
            "strconv.FormatUint(got.PackageBytes, 10) != row.PackageBytes) {"
        ),
        (
            "if false && !got.Withdrawn && (got.Manifest != row.Manifest || "
            "strconv.FormatUint(got.PackageBytes, 10) != row.PackageBytes) {"
        ),
        "services/indexer/internal/reconcile",
        ("TestAlteredProjectionIsDetected",),
    ),
    (
        "query-error-is-empty",
        "services/indexer/internal/reconcile/reconcile.go",
        "if snap.Err != nil {\n\t\treturn nil\n\t}",
        "if false && snap.Err != nil {\n\t\treturn nil\n\t}",
        "services/indexer/internal/reconcile",
        ("TestQueryErrorIsNotEmptyHistory",),
    ),
    (
        "commit-age-ignored",
        "services/indexer/internal/status/status.go",
        "if age > in.StaleAfter {",
        "if age < in.StaleAfter && false {",
        "services/indexer/internal/status",
        ("TestStoppedChainIsStaleWithoutARenewal",),
    ),
    (
        "index-above-floor",
        "services/indexer/internal/engine/engine.go",
        "for height := next; height <= floor; height++ {",
        "for height := next; height <= floor+1; height++ {",
        "services/indexer/internal/engine",
        ("TestDoesNotIndexAboveTheFloor",),
    ),
    (
        "missing-height-skipped",
        "services/indexer/internal/engine/engine.go",
        "return Result{Indexed: height - 1, Floor: floor, Gap: gap}, err",
        "continue",
        "services/indexer/internal/engine",
        ("TestMissingHeightStopsThePass",),
    ),
    (
        "bad-version-skipped",
        "services/indexer/internal/engine/engine.go",
        ('if errors.Is(err, project.ErrEventVersion) {\n\t\t\t\tcode = "event_version"\n\t\t\t}'),
        (
            "if errors.Is(err, project.ErrEventVersion) {\n"
            '\t\t\t\tcode = "event_version"\n'
            "\t\t\t\terr = nil\n"
            "\t\t\t}"
        ),
        "services/indexer/internal/engine",
        ("TestBadVersionDoesNotAdvance",),
    ),
]


def unique(text: str, old: str, label: str) -> str | None:
    count = text.count(old)
    if count != 1:
        return f"{label}: search string occurs {count} times"
    return None


def run_tests(
    copy: Path, package: str, tests: tuple[str, ...], timeout: int
) -> subprocess.CompletedProcess[str]:
    pattern = "^(" + "|".join(tests) + ")$"
    env = os.environ.copy()
    env["GOPROXY"] = "off"
    env["GOFLAGS"] = ""
    return subprocess.run(
        ["go", "test", "-v", "-count=1", "-timeout", "3m", "-run", pattern, "./" + package],
        cwd=copy,
        env=env,
        text=True,
        capture_output=True,
        timeout=timeout,
        check=False,
    )


def killed(output: str, tests: tuple[str, ...]) -> bool:
    if "build failed" in output or "[build failed]" in output:
        return False
    return any(f"--- FAIL: {name}" in output for name in tests)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--rev", default="HEAD")
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--list", action="store_true")
    parser.add_argument("--timeout", type=int, default=180)
    args = parser.parse_args()
    if args.list:
        for row in MUTANTS:
            print(row[0], row[4], ",".join(row[5]))
        return 0
    problems: list[str] = []
    for mid, path, old, _new, pkg, tests in MUTANTS:
        file = ROOT / path
        if not file.is_file():
            problems.append(f"{mid}: missing {path}")
            continue
        err = unique(file.read_text(encoding="utf-8"), old, mid)
        if err:
            problems.append(err)
        for name in tests:
            found = False
            for test_file in (ROOT / pkg).glob("*_test.go"):
                if f"func {name}(" in test_file.read_text(encoding="utf-8"):
                    found = True
            if not found:
                problems.append(f"{mid}: killer {name} is not defined")
    if problems:
        print("\n".join(problems))
        return 1
    if args.check:
        print(f"RESULT check mutants={len(MUTANTS)}")
        return 0

    tmp = Path(tempfile.mkdtemp(prefix="svn-mut-12-"))
    try:
        archive = subprocess.run(
            ["git", "archive", args.rev], cwd=ROOT, check=True, capture_output=True
        )
        subprocess.run(["tar", "-x", "-C", str(tmp)], input=archive.stdout, check=True)
        packages: dict[str, set[str]] = {}
        for _mid, _path, _old, _new, pkg, tests in MUTANTS:
            packages.setdefault(pkg, set()).update(tests)
        for pkg, tests in packages.items():
            proc = run_tests(tmp, pkg, tuple(sorted(tests)), args.timeout)
            out = proc.stdout + proc.stderr
            if proc.returncode != 0 or any(f"--- PASS: {name}" not in out for name in tests):
                print(out)
                print(f"control failed for {pkg}")
                return 1
        killed_n = 0
        for mid, path, old, new, pkg, tests in MUTANTS:
            target = tmp / path
            original = target.read_text(encoding="utf-8")
            target.write_text(original.replace(old, new, 1), encoding="utf-8")
            try:
                proc = run_tests(tmp, pkg, tests, args.timeout)
            except subprocess.TimeoutExpired:
                print(f"{mid} TIMEOUT")
                return 1
            finally:
                target.write_text(original, encoding="utf-8")
            out = proc.stdout + proc.stderr
            if killed(out, tests):
                killed_n += 1
                print(f"{mid} KILLED")
                continue
            print(out[-4000:])
            print(f"{mid} NOT KILLED exit={proc.returncode}")
            return 1
        print(f"RESULT mutants={len(MUTANTS)} killed={killed_n}")
        return 0 if killed_n == len(MUTANTS) else 1
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
