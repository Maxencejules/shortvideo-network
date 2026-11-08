"""The CI go job may skip the chain/devnet tests only while go-race runs all of them.

The go job sets SVN_SKIP_DEVNET=1 so that its non-race run of the multi-process
devnet tests does not reach the 20-minute job cap. Those tests then run only in
the go-race "devnet" and "devnet-evidence" shards. This guard fails when the
skip is set and those two shards no longer run every chain/devnet test under
-race, or could pass without running them or without failing when they fail:
an operator or -args after go test, -exec, -count below 1, GOFLAGS, an `if:`,
continue-on-error, a changed shell, `set +e`, or a trap. It reads the workflow
text with its own small parser, not a shell, so it guards against edits like
the mutations below; it does not prove that no shell construct can hide a
failure. The mutated documents below are fixtures, not workflow files.
"""

from __future__ import annotations

import fnmatch
import re
import shlex
from dataclasses import dataclass
from pathlib import Path

import pytest
import yaml

ROOT = Path(__file__).resolve().parents[2]
CI = ROOT / ".github" / "workflows" / "ci.yml"
DEVNET_DIR = ROOT / "chain" / "devnet"

SKIP_ENV = "SVN_SKIP_DEVNET"
GO_JOB = "go"
RACE_JOB = "go-race"
PACKAGE_ARGS = frozenset({"./chain/devnet", "./chain/devnet/"})
# Shard name -> the filter flag it must use. Both use the same regex, so a
# top-level test runs in "devnet-evidence" when the regex matches its name and
# in "devnet" otherwise: together they run the whole package once.
SHARD_FILTERS = {"devnet": "skip", "devnet-evidence": "run"}
# go test flags that take a value. A flag missing here is read as boolean,
# which can only make a package argument look absent (fail closed).
VALUE_FLAGS = frozenset(
    {
        "C",
        "bench",
        "benchtime",
        "blockprofile",
        "count",
        "covermode",
        "coverpkg",
        "coverprofile",
        "cpu",
        "cpuprofile",
        "exec",
        "list",
        "memprofile",
        "o",
        "outputdir",
        "p",
        "parallel",
        "run",
        "shuffle",
        "skip",
        "tags",
        "timeout",
        "vet",
    }
)
# Flags with which go test compiles, lists or prints without running every
# selected test, runs the test binary through another program (-exec true
# reports ok without running it), or with which a test may skip itself.
NOT_RUNNING_FLAGS = frozenset({"c", "exec", "list", "n", "short"})
# Environment variables that change what a shard's go test runs: GOFLAGS can
# add -run=^$, -exec, or -count=0 to every go command.
RACE_FORBIDDEN_ENV = (SKIP_ENV, "GOFLAGS")
_SHELL_OPERATOR = re.compile(r"[();<>|&]+")
# `set +e` (or +xe, +o errexit) lets the script go on after a failing go
# test; an EXIT trap can replace the exit code.
_EXIT_CODE_CHANGE = re.compile(r"\bset\s+\+\w*e|\bset\s+\+o\s+errexit\b|\btrap\b")
_CASE = re.compile(r'^\s*case\s+"?\$\{?SHARD\}?"?\s+in\s*$', re.MULTILINE)
_LABEL = re.compile(r"""^\s*\(?([A-Za-z0-9_*?|"'-]+)\)\s*(.*)$""")
_SKIP_GATE = re.compile(r'if os\.Getenv\("(\w+)"\) == "1" \{\s*t\.Skip\(')


@dataclass(frozen=True)
class GoTest:
    """One `go test` command line of a shard."""

    flags: dict[str, str]
    packages: tuple[str, ...]
    # tail is everything from the first shell operator (||, &&, |, ;, &,
    # a redirection) or -args on. Any tail is refused: `|| true` hides a
    # failure, a pipe hides the exit code (the default bash -e has no
    # pipefail), and arguments after -args reach the test binary
    # (-test.run=^$ runs nothing).
    tail: tuple[str, ...] = ()


def _mapping(value: object) -> dict[str, object]:
    return value if isinstance(value, dict) else {}


def _env(holder: object) -> dict[str, object]:
    return _mapping(_mapping(holder).get("env"))


def _steps(job: dict[str, object]) -> list[dict[str, object]]:
    steps = job.get("steps")
    if not isinstance(steps, list):
        return []
    return [step for step in steps if isinstance(step, dict)]


def names_env(data: dict[str, object], job: dict[str, object], name: str) -> bool:
    """True when the workflow, the job, or one of its steps names variable `name`.

    Any value counts, and so does any mention in a run script (an export, an
    inline assignment, `go env -w`, or a write to GITHUB_ENV): this reads
    fail closed.
    """
    if name in _env(data) or name in _env(job):
        return True
    for step in _steps(job):
        if name in _env(step):
            return True
        run = step.get("run")
        if isinstance(run, str) and name in run:
            return True
    return False


def names_skip_env(data: dict[str, object], job: dict[str, object]) -> bool:
    """True when the workflow, the job, or one of its steps names SVN_SKIP_DEVNET."""
    return names_env(data, job, SKIP_ENV)


def _logical_lines(script: str) -> list[str]:
    lines: list[str] = []
    pending = ""
    for raw in script.splitlines():
        if raw.rstrip().endswith("\\"):
            pending += raw.rstrip()[:-1] + " "
            continue
        lines.append(pending + raw)
        pending = ""
    if pending:
        lines.append(pending)
    return lines


@dataclass(frozen=True)
class CaseStatement:
    """The branches of a `case "${SHARD}" in` statement, in source order."""

    branches: tuple[tuple[tuple[str, ...], tuple[str, ...]], ...]
    unparsed: tuple[str, ...]

    def branch(self, shard: str) -> list[str] | None:
        """The body that the shell runs for `shard`: the first label matching it."""
        for labels, body in self.branches:
            if any(fnmatch.fnmatchcase(shard, label) for label in labels):
                return list(body)
        return None


def case_statement(script: str) -> CaseStatement:
    """Parse the `case "${SHARD}" in` statement of a run script."""
    match = _CASE.search(script)
    if match is None:
        return CaseStatement(branches=(), unparsed=("no case statement on SHARD",))
    branches: list[tuple[tuple[str, ...], tuple[str, ...]]] = []
    unparsed: list[str] = []
    labels: tuple[str, ...] | None = None
    body: list[str] = []
    for line in _logical_lines(script[match.end() :]):
        stripped = line.strip()
        if labels is None:
            if stripped == "esac":
                break
            if not stripped or stripped.startswith("#"):
                continue
            label = _LABEL.match(line)
            if label is None:
                unparsed.append(stripped)
                continue
            labels = tuple(part.strip("\"'") for part in label.group(1).split("|"))
            body = []
            rest = label.group(2).strip()
            if not rest:
                continue
            stripped = rest
        end = stripped.endswith(";;")
        if end:
            stripped = stripped[:-2].strip()
        if stripped:
            body.append(stripped)
        if end:
            branches.append((labels, tuple(body)))
            labels = None
    if labels is not None:
        unparsed.append("unterminated branch " + "|".join(labels))
    return CaseStatement(branches=tuple(branches), unparsed=tuple(unparsed))


def _flag(token: str) -> tuple[str, str | None]:
    name = token.lstrip("-")
    value: str | None = None
    if "=" in name:
        name, value = name.split("=", 1)
    if name.startswith("test."):
        name = name[len("test.") :]
    return name, value


def _tokens(line: str) -> list[str]:
    """Shell words of a line, with each run of operator characters a word."""
    lexer = shlex.shlex(line, posix=True, punctuation_chars=True)
    lexer.whitespace_split = True
    lexer.commenters = "#"
    return list(lexer)


def parse_go_test(line: str) -> GoTest | None:
    """The flags, package arguments, and refused tail of a `go test` line, or None."""
    try:
        tokens = _tokens(line)
    except ValueError:
        return None
    start = 0
    while start < len(tokens) and re.fullmatch(r"[A-Za-z_]\w*=.*", tokens[start]):
        start += 1
    if tokens[start : start + 2] != ["go", "test"]:
        return None
    flags: dict[str, str] = {}
    packages: list[str] = []
    rest = tokens[start + 2 :]
    index = 0
    while index < len(rest):
        token = rest[index]
        if token == "-args" or _SHELL_OPERATOR.fullmatch(token):
            return GoTest(flags=flags, packages=tuple(packages), tail=tuple(rest[index:]))
        index += 1
        if not token.startswith("-"):
            packages.append(token)
            continue
        name, value = _flag(token)
        if value is None and name in VALUE_FLAGS:
            value = rest[index] if index < len(rest) else ""
            if _SHELL_OPERATOR.fullmatch(value):
                return GoTest(flags=flags, packages=tuple(packages), tail=tuple(rest[index:]))
            index += 1
        flags[name] = "true" if value is None else value
    return GoTest(flags=flags, packages=tuple(packages))


def _count_problem(flags: dict[str, str]) -> str | None:
    """A problem when -count is set below 1 (-count=0 runs no test and reports ok)."""
    if "count" not in flags:
        return None
    try:
        count = int(flags["count"])
    except ValueError:
        return f"-count {flags['count']!r} is not an integer"
    return None if count >= 1 else f"-count {count} below 1"


def _shard_filter(shard: str, lines: list[str]) -> tuple[str | None, list[str]]:
    """The filter regex of the shard's race run of ./chain/devnet, and errors."""
    want = SHARD_FILTERS[shard]
    other = "run" if want == "skip" else "skip"
    errors: list[str] = []
    found: list[str] = []
    for line in lines:
        command = parse_go_test(line)
        if command is None or PACKAGE_ARGS.isdisjoint(command.packages):
            continue
        problems: list[str] = []
        if command.flags.get("race") not in ("true", "1"):
            problems.append("no -race")
        if other in command.flags:
            problems.append(f"-{other} present")
        if want not in command.flags or not command.flags[want]:
            problems.append(f"no -{want}")
        blocked = sorted(NOT_RUNNING_FLAGS.intersection(command.flags))
        if blocked:
            problems.append("flags " + ", ".join("-" + name for name in blocked))
        count = _count_problem(command.flags)
        if count is not None:
            problems.append(count)
        if len(command.packages) != 1:
            problems.append(f"package arguments {list(command.packages)} besides ./chain/devnet")
        if command.tail:
            problems.append("shell operator or -args after go test: " + " ".join(command.tail))
        if problems:
            errors.append(
                f"{RACE_JOB} shard {shard}: go test of ./chain/devnet: " + "; ".join(problems)
            )
            continue
        found.append(command.flags[want])
    if not found:
        errors.append(
            f"{RACE_JOB} shard {shard}: no `go test -race -{want} <regex> ./chain/devnet` command"
        )
        return None, errors
    if len(set(found)) != 1:
        errors.append(f"{RACE_JOB} shard {shard}: several -{want} filters {sorted(set(found))}")
        return None, errors
    return found[0], errors


def _race_errors(data: dict[str, object], jobs: dict[str, object]) -> list[str]:
    raw = jobs.get(RACE_JOB)
    if not isinstance(raw, dict):
        return [f"no {RACE_JOB} job"]
    job: dict[str, object] = raw
    errors: list[str] = []
    if "if" in job:
        errors.append(f"{RACE_JOB} job has an if condition")
    if "continue-on-error" in job:
        errors.append(f"{RACE_JOB} job has continue-on-error")
    if "defaults" in job or "defaults" in data:
        errors.append(f"{RACE_JOB} job or workflow sets defaults (the run shell)")
    for name in RACE_FORBIDDEN_ENV:
        if names_env(data, job, name):
            errors.append(f"{RACE_JOB} job or workflow names {name}")
    matrix = _mapping(_mapping(job.get("strategy")).get("matrix"))
    shards = matrix.get("shard")
    shard_list = [str(shard) for shard in shards] if isinstance(shards, list) else []
    for shard in SHARD_FILTERS:
        if shard not in shard_list:
            errors.append(f"{RACE_JOB} matrix has no {shard} shard")
    excludes = matrix.get("exclude")
    if excludes is not None:
        for entry in excludes if isinstance(excludes, list) else [excludes]:
            if str(_mapping(entry).get("shard")) in SHARD_FILTERS or not isinstance(entry, dict):
                errors.append(f"{RACE_JOB} matrix excludes {entry}")
    steps = [
        step
        for step in _steps(job)
        if str(_env(step).get("SHARD", "")).replace(" ", "") == "${{matrix.shard}}"
    ]
    if len(steps) != 1:
        return errors + [f"{RACE_JOB} has {len(steps)} steps with SHARD from matrix.shard"]
    step = steps[0]
    if "if" in step:
        errors.append(f"{RACE_JOB} race step has an if condition")
    if "continue-on-error" in step:
        errors.append(f"{RACE_JOB} race step has continue-on-error")
    if "shell" in step:
        errors.append(f"{RACE_JOB} race step sets its shell")
    run = step.get("run")
    script = run if isinstance(run, str) else ""
    if _EXIT_CODE_CHANGE.search(script):
        errors.append(f"{RACE_JOB} race step turns errexit off or sets a trap")
    case = case_statement(script)
    errors.extend(f"{RACE_JOB} race step case: {line}" for line in case.unparsed)
    filters: dict[str, str] = {}
    for shard in SHARD_FILTERS:
        lines = case.branch(shard)
        if lines is None:
            errors.append(f"{RACE_JOB} race step has no case branch for {shard}")
            continue
        regex, shard_errors = _shard_filter(shard, lines)
        errors.extend(shard_errors)
        if regex is not None:
            filters[shard] = regex
    if len(filters) == len(SHARD_FILTERS):
        skip, run_regex = filters["devnet"], filters["devnet-evidence"]
        if skip != run_regex:
            errors.append(
                f"devnet -skip {skip!r} and devnet-evidence -run {run_regex!r} differ, "
                "so together they need not cover chain/devnet"
            )
        elif "/" in skip:
            errors.append(f"filter {skip!r} has a slash, so it also selects subtests")
    return errors


def devnet_coverage_errors(text: str) -> list[str]:
    """Errors when the go job skips chain/devnet and go-race does not run all of it."""
    data = yaml.safe_load(text)
    if not isinstance(data, dict):
        return ["workflow is not a mapping"]
    jobs = _mapping(data.get("jobs"))
    go_job = jobs.get(GO_JOB)
    if isinstance(go_job, dict) and not names_skip_env(data, go_job):
        return []
    return _race_errors(data, jobs)


def go_job_skips_devnet(text: str) -> bool:
    data = yaml.safe_load(text)
    go_job = _mapping(_mapping(data).get("jobs")).get(GO_JOB)
    return isinstance(go_job, dict) and names_skip_env(data, go_job)


def _real() -> str:
    return CI.read_text(encoding="utf-8")


def test_real_workflow_runs_every_devnet_test_under_race() -> None:
    text = _real()
    # The go job skips chain/devnet today, so the race shard checks below are
    # the ones that ran. If the go job runs the devnet tests again, this line
    # changes with it.
    assert go_job_skips_devnet(text)
    assert devnet_coverage_errors(text) == []


def test_real_go_step_sets_the_skip_and_says_where_the_tests_run() -> None:
    data = yaml.safe_load(_real())
    steps = _steps(_mapping(data["jobs"][GO_JOB]))
    [step] = [step for step in steps if step.get("name") == "Format, generate, and test"]
    assert _env(step) == {SKIP_ENV: "1"}
    run = step.get("run")
    assert isinstance(run, str)
    lines = run.splitlines()
    echo = [
        i for i, line in enumerate(lines) if line.strip().startswith("echo") and "go-race" in line
    ]
    call = [i for i, line in enumerate(lines) if "scripts/run_tests.py --only go" in line]
    assert len(echo) == 1 and len(call) == 1 and echo[0] < call[0]
    assert "devnet-evidence" in lines[echo[0]]


def test_devnet_skip_gates_use_the_guarded_variable() -> None:
    gates: dict[str, set[str]] = {}
    for path in sorted(DEVNET_DIR.glob("*_test.go")):
        for name in _SKIP_GATE.findall(path.read_text(encoding="utf-8")):
            gates.setdefault(name, set()).add(path.name)
    assert set(gates) == {SKIP_ENV}, gates


MUTATIONS = [
    pytest.param(
        "shard: [app, devnet, devnet-evidence, rest]",
        "shard: [app, devnet, rest]",
        "no devnet-evidence shard",
        id="matrix-drops-devnet-evidence",
    ),
    pytest.param(
        "shard: [app, devnet, devnet-evidence, rest]",
        "shard: [app, devnet-evidence, rest]",
        "no devnet shard",
        id="matrix-drops-devnet",
    ),
    pytest.param(
        "-skip TestDevnetAudit ./chain/devnet",
        "-skip TestDevnetAuditLifecycle ./chain/devnet",
        "differ",
        id="skip-and-run-regex-differ",
    ),
    pytest.param(
        "-skip TestDevnetAudit ./chain/devnet",
        "-run TestDevnetAudit ./chain/devnet",
        "no -skip",
        id="devnet-shard-uses-run",
    ),
    pytest.param(
        "-run TestDevnetAudit ./chain/devnet",
        "-run TestDevnetAudit -skip TestDevnetAuditLifecycle ./chain/devnet",
        "-skip present",
        id="evidence-shard-adds-skip",
    ),
    pytest.param(
        "go test -race -count=1 -timeout 15m -run TestDevnetAudit",
        "go test -count=1 -timeout 15m -run TestDevnetAudit",
        "no -race",
        id="evidence-shard-drops-race",
    ),
    pytest.param(
        "-skip TestDevnetAudit ./chain/devnet",
        "-skip TestDevnetAudit ./chain/app",
        "shard devnet: no `go test",
        id="devnet-shard-wrong-package",
    ),
    pytest.param(
        "              go test -race -count=1 -timeout 12m -skip TestDevnetAudit",
        "              # go test -race -count=1 -timeout 12m -skip TestDevnetAudit",
        "shard devnet: no `go test",
        id="devnet-shard-commented-out",
    ),
    pytest.param(
        "go test -race -count=1 -timeout 12m -skip TestDevnetAudit",
        "go test -race -short -count=1 -timeout 12m -skip TestDevnetAudit",
        "flags -short",
        id="devnet-shard-short",
    ),
    pytest.param(
        "go test -race -count=1 -timeout 15m -run TestDevnetAudit",
        "go test -race -list . -count=1 -timeout 15m -run TestDevnetAudit",
        "flags -list",
        id="evidence-shard-list-only",
    ),
    pytest.param(
        "          SHARD: ${{ matrix.shard }}\n",
        '          SHARD: ${{ matrix.shard }}\n          SVN_SKIP_DEVNET: "1"\n',
        "go-race job or workflow names SVN_SKIP_DEVNET",
        id="race-step-env-sets-skip",
    ),
    pytest.param(
        "        shard: [app, devnet, devnet-evidence, rest]\n",
        "        shard: [app, devnet, devnet-evidence, rest]\n    env:\n      SVN_SKIP_DEVNET: 1\n",
        "go-race job or workflow names SVN_SKIP_DEVNET",
        id="race-job-env-sets-skip",
    ),
    pytest.param(
        "permissions:\n  contents: read\n",
        'permissions:\n  contents: read\nenv:\n  SVN_SKIP_DEVNET: "1"\n',
        "go-race job or workflow names SVN_SKIP_DEVNET",
        id="workflow-env-sets-skip",
    ),
    pytest.param(
        '          mod="github.com/Maxencejules/shortvideo-network"\n',
        "          export SVN_SKIP_DEVNET=1\n"
        '          mod="github.com/Maxencejules/shortvideo-network"\n',
        "go-race job or workflow names SVN_SKIP_DEVNET",
        id="race-run-exports-skip",
    ),
    pytest.param(
        "  go-race:\n    runs-on: ubuntu-24.04\n",
        "  go-race:\n    if: false\n    runs-on: ubuntu-24.04\n",
        "go-race job has an if condition",
        id="race-job-disabled",
    ),
    pytest.param(
        "            devnet-evidence)\n",
        "            devnet-audit)\n",
        # The shell then runs the "*)" branch, which only fails the job.
        "shard devnet-evidence: no `go test",
        id="evidence-branch-renamed",
    ),
    pytest.param(
        "          SHARD: ${{ matrix.shard }}\n",
        "          SHARD: app\n",
        "0 steps with SHARD from matrix.shard",
        id="race-step-ignores-matrix",
    ),
    # Edits that keep both go test lines but hide a failure or run no test.
    pytest.param(
        "-skip TestDevnetAudit ./chain/devnet",
        "-skip TestDevnetAudit ./chain/devnet || true",
        "shard devnet: go test of ./chain/devnet: shell operator or -args after go test: || true",
        id="devnet-shard-or-true",
    ),
    pytest.param(
        "-skip TestDevnetAudit ./chain/devnet",
        "-skip TestDevnetAudit ./chain/devnet||true",
        "shell operator or -args after go test: || true",
        id="devnet-shard-or-true-unspaced",
    ),
    pytest.param(
        "-run TestDevnetAudit ./chain/devnet",
        "-run TestDevnetAudit ./chain/devnet || true",
        "shard devnet-evidence: go test of ./chain/devnet: shell operator or -args",
        id="evidence-shard-or-true",
    ),
    pytest.param(
        "-run TestDevnetAudit ./chain/devnet",
        "-run TestDevnetAudit ./chain/devnet 2>&1 | tee devnet-evidence.log",
        "shell operator or -args after go test: >& 1 | tee devnet-evidence.log",
        id="evidence-shard-piped-to-tee",
    ),
    pytest.param(
        "-run TestDevnetAudit ./chain/devnet",
        "-run TestDevnetAudit ./chain/devnet -args -test.run=^$",
        "shell operator or -args after go test: -args -test.run=^$",
        id="evidence-shard-args-run-nothing",
    ),
    pytest.param(
        "-skip TestDevnetAudit ./chain/devnet",
        "-skip TestDevnetAudit ./chain/devnet ./chain/app",
        "package arguments",
        id="devnet-shard-extra-package",
    ),
    pytest.param(
        "go test -race -count=1 -timeout 12m -skip TestDevnetAudit",
        "go test -race -count=0 -timeout 12m -skip TestDevnetAudit",
        "-count 0 below 1",
        id="devnet-shard-count-zero",
    ),
    pytest.param(
        "go test -race -count=1 -timeout 15m -run TestDevnetAudit",
        "go test -race -exec true -count=1 -timeout 15m -run TestDevnetAudit",
        "flags -exec",
        id="evidence-shard-exec-true",
    ),
    pytest.param(
        "  go-race:\n    runs-on: ubuntu-24.04\n",
        "  go-race:\n    continue-on-error: true\n    runs-on: ubuntu-24.04\n",
        "go-race job has continue-on-error",
        id="race-job-continue-on-error",
    ),
    pytest.param(
        "      - name: Race tests\n",
        "      - name: Race tests\n        continue-on-error: true\n",
        "go-race race step has continue-on-error",
        id="race-step-continue-on-error",
    ),
    pytest.param(
        "        shard: [app, devnet, devnet-evidence, rest]\n",
        "        shard: [app, devnet, devnet-evidence, rest]\n    env:\n      GOFLAGS: -run=^$\n",
        "go-race job or workflow names GOFLAGS",
        id="race-job-env-goflags",
    ),
    pytest.param(
        "          SHARD: ${{ matrix.shard }}\n",
        '          SHARD: ${{ matrix.shard }}\n          GOFLAGS: "-count=0"\n',
        "go-race job or workflow names GOFLAGS",
        id="race-step-env-goflags",
    ),
    pytest.param(
        '          mod="github.com/Maxencejules/shortvideo-network"\n',
        "          export GOFLAGS=-run=^$\n"
        '          mod="github.com/Maxencejules/shortvideo-network"\n',
        "go-race job or workflow names GOFLAGS",
        id="race-run-exports-goflags",
    ),
    pytest.param(
        '          mod="github.com/Maxencejules/shortvideo-network"\n',
        '          set +e\n          mod="github.com/Maxencejules/shortvideo-network"\n',
        "race step turns errexit off or sets a trap",
        id="race-run-errexit-off",
    ),
    pytest.param(
        '          mod="github.com/Maxencejules/shortvideo-network"\n',
        "          trap 'exit 0' EXIT\n"
        '          mod="github.com/Maxencejules/shortvideo-network"\n',
        "race step turns errexit off or sets a trap",
        id="race-run-exit-trap",
    ),
    pytest.param(
        "      - name: Race tests\n",
        "      - name: Race tests\n        shell: bash {0}\n",
        "go-race race step sets its shell",
        id="race-step-shell-without-errexit",
    ),
    pytest.param(
        "permissions:\n  contents: read\n",
        "permissions:\n  contents: read\ndefaults:\n  run:\n    shell: bash {0}\n",
        "go-race job or workflow sets defaults",
        id="workflow-defaults-shell",
    ),
]


@pytest.mark.parametrize(("old", "new", "needle"), MUTATIONS)
def test_guard_fires_on_mutated_workflow(old: str, new: str, needle: str) -> None:
    text = _real()
    assert text.count(old) == 1, old
    mutated = text.replace(old, new)
    errors = devnet_coverage_errors(mutated)
    assert errors, new
    assert any(needle in error for error in errors), errors


def test_guard_fires_on_slash_in_both_filters() -> None:
    text = _real()
    for old, new in (
        ("-skip TestDevnetAudit ./chain/devnet", "-skip TestDevnetAudit/x ./chain/devnet"),
        ("-run TestDevnetAudit ./chain/devnet", "-run TestDevnetAudit/x ./chain/devnet"),
    ):
        assert text.count(old) == 1, old
        text = text.replace(old, new)
    errors = devnet_coverage_errors(text)
    assert any("slash" in error for error in errors), errors


def _without_go_skip(text: str) -> str:
    """The workflow with the go job's step env and its echo line removed."""
    step_env = '        env:\n          SVN_SKIP_DEVNET: "1"\n'
    assert text.count(step_env) == 1
    text = text.replace(step_env, "")
    echo = '          echo "SVN_SKIP_DEVNET=1:'
    assert text.count(echo) == 1
    start = text.index(echo)
    return text[:start] + text[text.index("\n", start) + 1 :]


def test_skip_written_in_the_go_run_script_still_needs_the_race_shards() -> None:
    text = _without_go_skip(_real())
    assert not go_job_skips_devnet(text)
    call = "          python3 scripts/run_tests.py --only go\n"
    assert text.count(call) == 1
    text = text.replace(
        call, "          SVN_SKIP_DEVNET=1 python3 scripts/run_tests.py --only go\n"
    )
    assert go_job_skips_devnet(text)
    assert devnet_coverage_errors(text) == []
    shards = "shard: [app, devnet, devnet-evidence, rest]"
    errors = devnet_coverage_errors(text.replace(shards, "shard: [app, devnet, rest]"))
    assert any("no devnet-evidence shard" in error for error in errors), errors


def test_without_the_skip_the_go_job_covers_devnet() -> None:
    text = _without_go_skip(_real())
    assert devnet_coverage_errors(text) == []
    without_shards = text.replace(
        "shard: [app, devnet, devnet-evidence, rest]", "shard: [app, rest]"
    )
    assert not go_job_skips_devnet(without_shards)
    assert devnet_coverage_errors(without_shards) == []


def test_case_statement_parses_inline_and_joined_lines() -> None:
    script = (
        'case "${SHARD}" in\n'
        "  a|b) go test -race \\\n"
        "      -skip X ./chain/devnet ;;\n"
        '  "c")\n'
        "    # comment\n"
        "    go test -race -run=X ./chain/devnet\n"
        "    ;;\n"
        "esac\n"
    )
    case = case_statement(script)
    assert case.unparsed == ()
    assert case.branch("a") == case.branch("b")
    joined = case.branch("a")
    assert joined is not None
    assert [" ".join(line.split()) for line in joined] == ["go test -race -skip X ./chain/devnet"]
    assert case.branch("c") == ["# comment", "go test -race -run=X ./chain/devnet"]
    parsed = parse_go_test("go test -race -run=X ./chain/devnet")
    assert parsed is not None
    assert parsed.flags == {"race": "true", "run": "X"}
    assert parsed.packages == ("./chain/devnet",)
    assert parsed.tail == ()
    piped = parse_go_test("go test -race -skip 'A|B' -timeout ; ./chain/devnet")
    assert piped is not None
    assert piped.flags == {"race": "true", "skip": "A|B"}
    assert piped.tail == (";", "./chain/devnet")
    assert parse_go_test("# go test -race ./chain/devnet") is None
    assert parse_go_test("true || go test -race -skip X ./chain/devnet") is None
    assert case.branch("d") is None


def test_case_statement_follows_shell_order() -> None:
    script = (
        'case "${SHARD}" in\n'
        "  dev*)\n"
        "    echo shadowed\n"
        "    ;;\n"
        "  devnet)\n"
        "    go test -race -skip X ./chain/devnet\n"
        "    ;;\n"
        "esac\n"
    )
    assert case_statement(script).branch("devnet") == ["echo shadowed"]


def test_guard_fires_when_an_earlier_branch_shadows_devnet() -> None:
    text = _real()
    old = "            devnet)\n"
    assert text.count(old) == 1
    mutated = text.replace(
        old, "            dev*)\n              echo shadowed\n              ;;\n" + old
    )
    errors = devnet_coverage_errors(mutated)
    assert any("shard devnet: no `go test" in error for error in errors), errors


def test_guard_fires_on_an_unparsed_case_label() -> None:
    text = _real()
    old = "            app)\n"
    assert text.count(old) == 1
    mutated = text.replace(old, "            app | other )\n")
    errors = devnet_coverage_errors(mutated)
    assert any("race step case: app | other )" in error for error in errors), errors


def test_guard_fires_when_no_branch_names_the_shard() -> None:
    text = _real()
    for old, new in (
        ("            devnet-evidence)\n", "            devnet-audit)\n"),
        (
            "            *)\n"
            '              echo "unknown shard ${SHARD}"\n'
            "              exit 1\n"
            "              ;;\n",
            "",
        ),
    ):
        assert text.count(old) == 1, old
        text = text.replace(old, new)
    errors = devnet_coverage_errors(text)
    assert any("no case branch for devnet-evidence" in error for error in errors), errors
