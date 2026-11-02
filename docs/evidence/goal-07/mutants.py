#!/usr/bin/env python3
"""Goal 07 mutation runner (standard library only).

What it does:
1. Exports a committed revision (default HEAD) with `git archive` into a new
   temporary directory outside the repository.
2. Runs every named test command once on the unmutated copy (control). A
   failing control stops the run.
3. For each mutant: replaces one exact search string in one file (the string
   must occur exactly once), runs the mutant's named non-devnet go test
   command in the copy, and restores the file.

Verdicts:
- KILLED: the command exited non-zero and the package built.
- SURVIVED: the command exited zero.
- BUILD_FAILED: a package or test binary did not build. This is not a kill.
- TIMEOUT: the command ran longer than --timeout seconds.

Exit status is 0 only when every control passed and every mutant is KILLED.
It is non-zero when a search string is missing or not unique, or when a
goal07 mutant does not reproduce its committed copy (both checked for all
selected mutants before any test runs), or when any mutant SURVIVED,
BUILD_FAILED or hit TIMEOUT.

Usage:
  python3 docs/evidence/goal-07/mutants.py [--rev REV] [--group goal07|merge]
                                            [--only a,b] [--check]
                                            [--logdir DIR] [--keep]

--check validates the search strings and the goal07 copies and runs nothing.
The copy runs Go with GOPROXY=off, so the module cache must already hold the
dependencies and the toolchain that go.work names. The copy uses its own
GOCACHE under the temporary directory, which is removed at the end unless
--keep is given.

Where the mutants come from.
- Group goal07 (m01 to m05): the five fix-round mutants of the goal 07
  evidence on commit 43c1a10. There, each was a full copy of one source file
  with one small change, docs/evidence/goal-07/mutants/<name>.go.txt, run by
  run_mutant.sh with go -overlay. The goal 06 merge rewrote
  chain/x/authority/msg.go, so the m05 copy no longer matches the tree. Here
  each mutant is the same one change as an exact search string. Before any
  test runs, the runner applies that change to the file as it was at
  43c1a10 and requires the result to equal the committed copy byte for byte,
  so the change is the one the earlier evidence ran. Each mutant runs the
  same named tests as run_mutant.sh.
- Group merge: mutants of the goal 06 merge into goal 07. The merge
  workflow ran them on its uncommitted worktree (scratch logs, not in Git):
  nine named in its gate fix-mutants log (genesis seat roles, the export
  catalogue check, the peer business-id gate, the content role check), and
  five hand-made mutants of chain/x/authority/msg.go named in its resolve
  report. Their source text was not kept. The search strings here are
  reconstructions from those names and descriptions, not the same text. The
  resolve report's sixth msg.go mutant (the peer lookup bound to the tx
  context instead of the message branch) is not listed: it survived there,
  and the report explains it as equivalent, because the engine never writes
  the peer stores. Each merge mutant runs the whole non-devnet packages
  that hold the merge's tests.
This runner re-runs all of them on the committed tree; its own output is the
evidence, not the scratch logs.
"""

from __future__ import annotations

import argparse
import io
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
from dataclasses import dataclass

# The revision whose files the goal07 copies were made from.
COPY_REV = "43c1a104d43ba55f2f58684a6f3790c0bb04c9f2"
COPY_DIR = "docs/evidence/goal-07/mutants"

SIM_GAS = "sim-gas"
QUERY_GAS = "query-gas"
ROTATION_CAP = "rotation-cap"
AUTHORITY_METER = "authority-meter"
MERGE = "merge"

# Named non-devnet test commands, run from the root of the copy. The first
# four are the test selections of run_mutant.sh.
COMMANDS: dict[str, tuple[list[str], dict[str, str]]] = {
    SIM_GAS: (["go", "test", "-count=1", "-run", "^TestSimulateEstimatesGas$", "./chain/app/"], {}),
    QUERY_GAS: (["go", "test", "-count=1", "-run", "^(TestQueryGasLimit|TestQueryGasLimitOption)$", "./chain/app/"], {}),
    ROTATION_CAP: (["go", "test", "-count=1", "-run", "^TestRotationCap$", "./chain/x/providers/"], {}),
    AUTHORITY_METER: (
        ["go", "test", "-count=1", "-run", "^(TestEnforceCommitted|TestHandlerMeterCheckByMode)$", "./chain/x/authority/"],
        {},
    ),
    MERGE: (
        [
            "go", "test", "-count=1", "-timeout", "10m",
            "./chain/x/authority/", "./chain/app/", "./chain/genesis/",
            "./chain/x/content/", "./chain/x/providers/", "./chain/cmd/svnd/",
        ],
        {"SVN_SKIP_DEVNET": "1"},
    ),
}

GROUPS = ("goal07", "merge")


@dataclass(frozen=True)
class Mutant:
    name: str
    group: str
    path: str
    old: str
    new: str
    command: str
    reason: str


MSG = "chain/x/authority/msg.go"
DOC = "chain/genesis/document.go"
AGEN = "chain/x/authority/genesis.go"
ENGINE = "chain/x/authority/engine.go"

MUTANTS: list[Mutant] = [
    # Group goal07: the fix-round mutants of the 43c1a10 evidence.
    Mutant(
        "m01-simulate-extra-gas", "goal07", "chain/msgkit/enforce.go",
        '\tctx.GasMeter().ConsumeGas(FixedMessageGas, "svn registry message")\n',
        '\tctx.GasMeter().ConsumeGas(FixedMessageGas, "svn registry message")\n'
        "\tif ctx.ExecMode() == sdk.ExecModeSimulate {\n"
        '\t\tctx.GasMeter().ConsumeGas(1, "mutant m01")\n'
        "\t}\n",
        SIM_GAS, "msgkit.Enforce consumes 1 extra gas in simulation; simulated gas must equal committed gas",
    ),
    Mutant(
        "m02-ante-gas0-always", "goal07", "chain/app/ante.go",
        "\tif !(simulate && limit == 0) && (limit < genesis.MinGasPerTx || limit > genesis.MaxGasPerTx) {\n",
        "\tif !(limit == 0) && (limit < genesis.MinGasPerTx || limit > genesis.MaxGasPerTx) {\n",
        SIM_GAS, "the ante handler must accept a declared gas limit of 0 only in simulation",
    ),
    Mutant(
        "m03-query-gas-zero-unlimited", "goal07", "chain/app/querygas.go",
        "\tif limit == 0 {\n\t\treturn DefaultQueryGasLimit, nil\n\t}\n\treturn limit, nil\n",
        "\treturn limit, nil\n",
        QUERY_GAS, "an absent or zero query_gas_limit must select the default, never 0 (SDK unlimited)",
    ),
    Mutant(
        "m04-rotation-cap-off-by-one", "goal07", "chain/x/providers/msg_server.go",
        "\tif record.GetKeyGeneration()-1 >= MaxRetiredKeysPerProvider {\n",
        "\tif record.GetKeyGeneration()-1 > MaxRetiredKeysPerProvider {\n",
        ROTATION_CAP, "a rotation past MaxRetiredKeysPerProvider retired keys must be BOUNDS",
    ),
    Mutant(
        "m05-authority-meter-finalize-only", "goal07", MSG,
        "\tif ctx.ExecMode() != sdk.ExecModeSimulate {\n",
        "\tif ctx.ExecMode() == sdk.ExecModeFinalize {\n",
        AUTHORITY_METER, "enforceCommitted must check the gas meter limit in every mode but simulation",
    ),
    # Group merge: genesis seat roles (DR-07-11).
    Mutant(
        "doc-seat-rule-off", "merge", DOC,
        "func validateSeatRoles(seats []DevnetTestSeat, rows []Submitter) error {\n",
        "func validateSeatRoles(seats []DevnetTestSeat, rows []Submitter) error {\n\tif true {\n\t\treturn nil\n\t}\n",
        MERGE, "genesis.Validate must refuse a seat address listed with a role other than BROADCASTER",
    ),
    Mutant(
        "auth-seat-rule-off", "merge", AGEN,
        "func validateSeatRoles(seats []*authorityv1.DevnetTestSeat, rows []*typesv1.Submitter) error {\n",
        "func validateSeatRoles(seats []*authorityv1.DevnetTestSeat, rows []*typesv1.Submitter) error {\n"
        "\tif true {\n\t\treturn nil\n\t}\n",
        MERGE, "authority ValidateGenesisState must refuse a seat address with a role other than BROADCASTER",
    ),
    Mutant(
        "auth-seat-rule-allows-auditor", "merge", AGEN,
        "&& row.GetRole() != typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER {\n",
        "&& row.GetRole() != typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER"
        " && row.GetRole() != typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR {\n",
        MERGE, "authority ValidateGenesisState must refuse a seat address listed as AUDITOR",
    ),
    Mutant(
        "doc-seat-rule-case-sensitive", "merge", DOC,
        "\t\t\tif strings.ToLower(row.AddressHex) == addr && row.Role != RoleBroadcaster {\n",
        "\t\t\tif row.AddressHex == addr && row.Role != RoleBroadcaster {\n",
        MERGE, "genesis.Validate must match a seat address case-insensitively",
    ),
    Mutant(
        "doc-seat-address-wrong", "merge", DOC,
        "\tsum := sha256.Sum256(pub)\n\treturn sum[:AccountBytes]\n",
        "\tsum := sha256.Sum256(pub)\n\treturn sum[1 : AccountBytes+1]\n",
        MERGE, "genesis.SeatAddress must derive the same address as the authority engine",
    ),
    # Group merge: the export catalogue check.
    Mutant(
        "export-no-catalogue-check", "merge", "chain/app/state.go",
        "\tif err := emptyCatalog(modules); err != nil {\n\t\treturn genesis.Document{}, err\n\t}\n",
        "",
        MERGE, "App.Export must refuse a non-empty provider or content catalogue",
    ),
    # Group merge: the peer business-id gate.
    Mutant(
        "peer-integrity-to-replay", "merge", ENGINE,
        "\tif len(prev) != sha256.Size {\n\t\treturn fail(CodeIntegrity)\n\t}\n",
        "\tif len(prev) != sha256.Size {\n\t\treturn fail(CodeReplay)\n\t}\n",
        MERGE, "a business-id value that is not 32 bytes must be INTEGRITY, not REPLAY",
    ),
    Mutant(
        "peer-empty-is-free", "merge", ENGINE,
        "\t\t\tprev = append([]byte{}, peer...) // non-nil, so an empty peer value is INTEGRITY below\n",
        "\t\t\tprev = peer\n",
        MERGE, "a nil peer business-id value must be INTEGRITY, not a free id",
    ),
    # Group merge: the content role check.
    Mutant(
        "content-accepts-broadcaster", "merge", "chain/x/content/msg_server.go",
        "\tcase typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE:\n\t\treturn msgkit.OK\n",
        "\tcase typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE, typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER:\n"
        "\t\treturn msgkit.OK\n",
        MERGE, "RegisterContent and WithdrawContent must be FORBIDDEN to a BROADCASTER signer",
    ),
    # Group merge: hand-made mutants of the authority transition.
    Mutant(
        "msg-keep-writes-registry", "merge", MSG,
        "\tcase out.Keep:\n\t\twrite()\n",
        "\tcase out.Keep:\n\t\twriteRegistry()\n\t\twrite()\n",
        MERGE, "a keep outcome (EXPIRED or REJECTED mark) must drop the registry branch",
    ),
    Mutant(
        "msg-hooks-on-message-branch", "merge", MSG,
        "\tst.Registry = k.registry(registryCtx)\n",
        "\t_ = registryCtx\n\tst.Registry = k.registry(cacheCtx)\n",
        MERGE, "the registry hooks must write to a cache nested in the message branch, not the message branch",
    ),
    Mutant(
        "msg-hooks-on-tx-context", "merge", MSG,
        "\tst.Registry = k.registry(registryCtx)\n",
        "\t_ = registryCtx\n\tst.Registry = k.registry(sdkCtx)\n",
        MERGE, "the registry hooks must not write to the transaction context",
    ),
    Mutant(
        "msg-success-skips-registry-write", "merge", MSG,
        "\tcase out.Success():\n\t\twriteRegistry()\n\t\twrite()\n",
        "\tcase out.Success():\n\t\t_ = writeRegistry\n\t\twrite()\n",
        MERGE, "a successful execute must write the registry branch",
    ),
    Mutant(
        "msg-simulate-skip-removed", "merge", MSG,
        "\tif ctx.ExecMode() != sdk.ExecModeSimulate {\n",
        "\tif true {\n",
        MERGE, "simulation must skip the meter-limit check so a client can estimate gas",
    ),
]

BUILD_FAILURE = re.compile(r"\[(build|setup) failed\]|^# \S+$|cannot find package|undefined: ", re.M)
FAIL_LINE = re.compile(r"^\s*--- FAIL: (\S+)", re.M)


def repo_root() -> pathlib.Path:
    here = pathlib.Path(__file__).resolve().parent
    out = subprocess.run(["git", "-C", str(here), "rev-parse", "--show-toplevel"],
                         capture_output=True, text=True, check=True)
    return pathlib.Path(out.stdout.strip())


def git(root: pathlib.Path, *args: str) -> str:
    return subprocess.run(["git", "-C", str(root), *args], capture_output=True, text=True, check=True).stdout.strip()


def git_bytes(root: pathlib.Path, *args: str) -> bytes | None:
    p = subprocess.run(["git", "-C", str(root), *args], capture_output=True)
    return p.stdout if p.returncode == 0 else None


def export(root: pathlib.Path, rev: str, dest: pathlib.Path) -> None:
    raw = subprocess.run(["git", "-C", str(root), "archive", "--format=tar", rev],
                         capture_output=True, check=True).stdout
    with tarfile.open(fileobj=io.BytesIO(raw), mode="r:") as tar:
        tar.extractall(dest, filter="data")


def validate(root: pathlib.Path, copy: pathlib.Path, mutants: list[Mutant]) -> tuple[list[str], list[str]]:
    problems: list[str] = []
    notes: list[str] = []
    names = set()
    for m in mutants:
        if m.name in names:
            problems.append(f"DUPLICATE_NAME {m.name}")
        names.add(m.name)
        if m.command not in COMMANDS:
            problems.append(f"UNKNOWN_COMMAND {m.name} {m.command}")
        if m.group not in GROUPS:
            problems.append(f"UNKNOWN_GROUP {m.name} {m.group}")
        if m.old == m.new:
            problems.append(f"NO_CHANGE {m.name}")
        target = copy / m.path
        if not target.is_file():
            problems.append(f"MISSING_FILE {m.name} {m.path}")
            continue
        n = target.read_text().count(m.old)
        if n != 1:
            problems.append(f"{'NOT_FOUND' if n == 0 else 'NOT_UNIQUE'} {m.name} {m.path} occurrences={n}")
        if m.group != "goal07":
            continue
        committed = copy / COPY_DIR / f"{m.name}.go.txt"
        before = git_bytes(root, "show", f"{COPY_REV}:{m.path}")
        if not committed.is_file() or before is None:
            problems.append(f"COPY_MISSING {m.name}")
            continue
        text = before.decode()
        if text.count(m.old) != 1:
            problems.append(f"COPY_ANCHOR {m.name} occurrences_at_{COPY_REV[:7]}={text.count(m.old)}")
            continue
        if text.replace(m.old, m.new, 1).encode() != committed.read_bytes():
            problems.append(f"COPY_DIFFERS {m.name}: the change applied to {m.path}@{COPY_REV[:7]} is not {COPY_DIR}/{m.name}.go.txt")
            continue
        same = (target.read_bytes() == before)
        notes.append(f"COPY_MATCH {m.name}: {m.path}@{COPY_REV[:7]} with this change equals {COPY_DIR}/{m.name}.go.txt; "
                     f"{m.path} {'unchanged' if same else 'changed'} since {COPY_REV[:7]}")
    return problems, notes


def run(copy: pathlib.Path, command: str, env: dict[str, str], timeout: int) -> tuple[str, int, str, float]:
    argv, extra = COMMANDS[command]
    t0 = time.monotonic()
    try:
        p = subprocess.run(argv, cwd=copy, env={**env, **extra}, capture_output=True, text=True, timeout=timeout)
    except subprocess.TimeoutExpired as exc:
        out = (exc.stdout or b"").decode(errors="replace") if isinstance(exc.stdout, bytes) else (exc.stdout or "")
        return "TIMEOUT", -1, out, time.monotonic() - t0
    out = p.stdout + p.stderr
    if p.returncode == 0:
        verdict = "SURVIVED"
    elif BUILD_FAILURE.search(out):
        verdict = "BUILD_FAILED"
    else:
        verdict = "KILLED"
    return verdict, p.returncode, out, time.monotonic() - t0


def main() -> int:
    ap = argparse.ArgumentParser(description=(__doc__ or "").splitlines()[0])
    ap.add_argument("--rev", default="HEAD")
    ap.add_argument("--group", default="", choices=("",) + GROUPS, help="run one group only")
    ap.add_argument("--only", default="", help="comma-separated mutant names")
    ap.add_argument("--check", action="store_true", help="validate search strings and copies only")
    ap.add_argument("--logdir", default="", help="write each command's full output here")
    ap.add_argument("--keep", action="store_true", help="keep the temporary copy and GOCACHE")
    ap.add_argument("--timeout", type=int, default=900, help="seconds per command")
    args = ap.parse_args()

    root = repo_root()
    commit = git(root, "rev-parse", f"{args.rev}^{{commit}}")
    tree = git(root, "rev-parse", f"{args.rev}^{{tree}}")
    only = {n for n in args.only.split(",") if n}
    unknown = only - {m.name for m in MUTANTS}
    if unknown:
        print(f"unknown mutant names: {sorted(unknown)}")
        return 2
    selected = [m for m in MUTANTS if (not only or m.name in only) and (not args.group or m.group == args.group)]
    if not selected:
        print("no mutant selected")
        return 2

    work = pathlib.Path(tempfile.mkdtemp(prefix="goal07-mutants-"))
    if work.resolve().is_relative_to(root.resolve()):
        print(f"temporary directory {work} is inside the repository")
        return 2
    copy = work / "tree"
    copy.mkdir()
    logdir = pathlib.Path(args.logdir).resolve() if args.logdir else None
    if logdir:
        logdir.mkdir(parents=True, exist_ok=True)
    print(f"revision {args.rev} commit {commit} tree {tree}")
    print(f"copy {copy} (git archive)")
    groups = ", ".join(f"{g}={sum(1 for m in selected if m.group == g)}" for g in GROUPS)
    print(f"mutants {len(selected)} of {len(MUTANTS)} ({groups})")
    try:
        export(root, commit, copy)
        problems, notes = validate(root, copy, selected)
        for line in notes:
            print(line)
        for line in problems:
            print(line)
        if problems:
            print(f"RESULT validation problems={len(problems)}; no test was run")
            return 1
        print("search strings: every one occurs exactly once")
        if args.check:
            return 0

        env = dict(os.environ, GOPROXY="off", GOCACHE=str(work / "gocache"))
        version = subprocess.run(["go", "version"], cwd=copy, capture_output=True, text=True, env=env)
        print(f"go {version.stdout.strip()} {version.stderr.strip()}".rstrip())
        used = sorted({m.command for m in selected})
        for name in used:
            argv, extra = COMMANDS[name]
            print(f"command {name}: {' '.join([f'{k}={v}' for k, v in extra.items()] + argv)}")

        for command in used:
            verdict, rc, out, secs = run(copy, command, env, args.timeout)
            if logdir:
                (logdir / f"control-{command}.txt").write_text(out)
            ok = verdict == "SURVIVED"
            print(f"CONTROL {command} {'PASS' if ok else 'FAIL'} exit={rc} secs={secs:.1f}", flush=True)
            if not ok:
                print(out[-4000:])
                print("RESULT control failed; mutants not run")
                return 1

        tally: dict[str, int] = {}
        bad = []
        for m in selected:
            target = copy / m.path
            original = target.read_text()
            target.write_text(original.replace(m.old, m.new, 1))
            try:
                verdict, rc, out, secs = run(copy, m.command, env, args.timeout)
            finally:
                target.write_text(original)
            tally[verdict] = tally.get(verdict, 0) + 1
            if logdir:
                (logdir / f"{m.name}.txt").write_text(out)
            fails = FAIL_LINE.findall(out)
            shown = " ".join(dict.fromkeys(fails))[:300]
            print(f"{verdict} {m.name} group={m.group} exit={rc} secs={secs:.1f} file={m.path} failing={shown or '-'}",
                  flush=True)
            print(f"    reason: {m.reason}", flush=True)
            if verdict != "KILLED":
                bad.append(m.name)
                print("    output tail:\n" + "\n".join("    " + line for line in out.strip().splitlines()[-25:]),
                      flush=True)
        summary = " ".join(f"{k.lower()}={v}" for k, v in sorted(tally.items()))
        print(f"RESULT mutants={len(selected)} {summary}")
        if bad:
            print(f"NOT KILLED: {', '.join(bad)}")
            return 1
        return 0
    finally:
        if args.keep:
            print(f"kept {work}")
        else:
            shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
