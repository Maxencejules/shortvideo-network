"""CI runs the services/broadcaster tests only where the pinned PostgreSQL is installed.

The module's tests start a disposable PostgreSQL 18.6 cluster from
/usr/lib/postgresql/18/bin (services/broadcaster/internal/pgtest), and the
broadcaster devnet tests of chain/devnet start one through the broadcaster.
No variable skips them (goal 11 plan section 12, DR-11-25). This guard fails
when:

- a job runs those tests (scripts/run_tests.py with the Go suite, a `make`
  target that runs it, a go test whose packages include services/broadcaster,
  or a go test of ./chain/devnet whose filter selects a TestDevnetBroadcaster
  test) and no earlier step of that job, or of that matrix shard, is the
  pinned install;
- an install step does not pin the goal 04 version and key fingerprint of
  scripts/env/install.sh and docs/dependencies.md, does not check the
  fingerprint or the installed version, can hide a failure, has a condition
  other than the shard list below, or differs from another install step;
- ci.yml, the Makefile or scripts/run_tests.py name a PostgreSQL skip
  variable, or a services/broadcaster or chain/devnet test reads one or skips
  itself after a PostgreSQL, binary or environment check;
- ci.yml names SVN_BROADCASTER_PHASES, the devnet phase selection that exists
  only for mutation killers (DR-11-36);
- the go-race shards broadcaster, devnet-broadcaster and
  devnet-broadcaster-stall are missing, or run without -race, -count=1 or an
  explicit -timeout of at most 15 minutes, or without their prebuilds;
- the go job does not vet the module in its default and both tagged builds,
  or does not run the svn_faultpoint tests of internal/faultpoint.

It reads the workflow with its own small parser, not a shell; a package
argument that is a shell variable (the computed ./chain/... list of the rest
shard) is read as outside the module. The mutated documents below are
fixtures, not workflow files. The last test checks the replacement of the
goal 03 Python scaffold by the Go module.
"""

from __future__ import annotations

import copy
import fnmatch
import json
import posixpath
import re
import shlex
from collections.abc import Callable, Mapping
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import pytest
import yaml
from scripts.run_tests import GO_MODULES

ROOT = Path(__file__).resolve().parents[2]
CI = ROOT / ".github" / "workflows" / "ci.yml"
MODULE = "services/broadcaster"
DEVNET = "chain/devnet"
GO_JOB = "go"
RACE_JOB = "go-race"
SHARD_RUN = {
    "devnet-broadcaster": "TestDevnetBroadcasterLifecycle",
    "devnet-broadcaster-stall": "TestDevnetBroadcasterQuorumStall",
}
MODULE_SHARD = "broadcaster"
# The go job vets the module in its default build and in both tagged
# builds, so the build-tagged test files compile in CI (the svn_roottest
# tests themselves need root and run only in the root capture), and runs
# the svn_faultpoint tests of internal/faultpoint, which only that tag
# compiles.
GO_JOB_LINES = (
    "go vet ./services/broadcaster/...",
    "go vet -tags svn_faultpoint ./services/broadcaster/...",
    "go vet -tags svn_roottest ./services/broadcaster/internal/signer/ "
    "./services/broadcaster/internal/keys/",
    "go test -count=1 -timeout 2m -tags svn_faultpoint ./services/broadcaster/internal/faultpoint/",
)
PREBUILDS = (
    "go build -C chain -o /dev/null ./cmd/svnd",
    "go build -C services/broadcaster -race -tags svn_faultpoint -o /dev/null "
    "./cmd/svn-broadcaster",
)
MAX_TIMEOUT_SECONDS = 15 * 60
# make targets that run scripts/run_tests.py with the Go suite.
MAKE_GO_TARGETS = frozenset({"unit-go", "test", "check"})
_PHASES = re.compile(r"SVN_BROADCASTER_\w*PHASE")
_IDENTIFIER = re.compile(r"\b[A-Za-z][A-Za-z0-9]*(?:_[A-Za-z0-9]+)+\b")
_SKIP_WORDS = frozenset({"SKIP", "NO", "DISABLE", "DISABLED", "WITHOUT", "OFF"})
_PG_WORDS = frozenset({"POSTGRES", "POSTGRESQL", "PG", "PGSQL", "PSQL", "DATABASE", "DB"})
_ENV_READ = re.compile(r"\b(?:Getenv|LookupEnv)\(\s*\"(\w+)\"")
_SKIP_CALL = re.compile(r"\.Skip(?:f|Now)?\(")
# A skip in the module after one of these is a skip on a missing PostgreSQL,
# a missing binary, or an environment variable.
_MODULE_SKIP_CAUSE = re.compile(
    r"(?i)postgres|\bpg_|pgtest|pgdev|initdb|BinDir|LookPath|Getenv|LookupEnv"
)
# chain/devnet keeps its SVN_SKIP_DEVNET gate (test_ci_devnet_coverage.py).
_DEVNET_SKIP_CAUSE = re.compile(r"(?i)postgres|\bpg_|pgtest|pgdev|initdb|BinDir|LookPath")
_IF_SHARDS = re.compile(r"^contains\(fromJSON\('(\[[^']*\])'\),\s*matrix\.shard\)$")
_CASE = re.compile(r'^\s*case\s+"?\$\{?SHARD\}?"?\s+in\s*$')
_LABEL = re.compile(r"^\s*\(?([^()\s]+)\)\s*(.*)$")
_OPERATOR = re.compile(r"[();<>|&]+")
_DURATION = re.compile(r"^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$")
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


@dataclass(frozen=True)
class GoTest:
    """One `go test` command: its flags, package arguments, directory and tail."""

    flags: dict[str, str]
    packages: tuple[str, ...]
    cwd: str
    tail: tuple[str, ...] = ()


@dataclass
class Commands:
    """What a run script does for one shard, as this guard reads it."""

    go_tests: list[GoTest] = field(default_factory=list)
    runs_go_suite: bool = False
    lines: list[str] = field(default_factory=list)


def _mapping(value: object) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def _steps(job: Mapping[str, Any]) -> list[dict[str, Any]]:
    steps = job.get("steps")
    if not isinstance(steps, list):
        return []
    return [step for step in steps if isinstance(step, dict)]


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


def shard_lines(script: str, shard: str | None) -> tuple[list[str], list[str]]:
    """The lines a run script runs for `shard`, and the lines this parser refuses.

    Lines outside a `case "${SHARD}" in` statement run for every shard. With no
    shard (a job without a matrix) every branch counts, which reads fail closed.
    """
    lines = _logical_lines(script)
    out: list[str] = []
    problems: list[str] = []
    index = 0
    while index < len(lines):
        line = lines[index]
        index += 1
        if not _CASE.match(line):
            out.append(line)
            continue
        chosen: list[str] | None = None
        labels: tuple[str, ...] | None = None
        body: list[str] = []
        while index < len(lines):
            stripped = lines[index].strip()
            index += 1
            if labels is None:
                if stripped == "esac":
                    break
                if not stripped or stripped.startswith("#"):
                    continue
                label = _LABEL.match(stripped)
                if label is None:
                    problems.append(stripped)
                    continue
                labels = tuple(part.strip("\"'") for part in label.group(1).split("|"))
                body = []
                stripped = label.group(2).strip()
                if not stripped:
                    continue
            end = stripped.endswith(";;")
            if end:
                stripped = stripped[:-2].strip()
            if stripped:
                body.append(stripped)
            if end:
                if shard is None:
                    out.extend(body)
                elif chosen is None and any(fnmatch.fnmatchcase(shard, lab) for lab in labels):
                    chosen = body
                labels = None
        if labels is not None:
            problems.append("unterminated case branch " + "|".join(labels))
        if chosen is not None:
            out.extend(chosen)
    return out, problems


def _tokens(line: str) -> list[str] | None:
    lexer = shlex.shlex(line, posix=True, punctuation_chars=True)
    lexer.whitespace_split = True
    lexer.commenters = "#"
    try:
        return list(lexer)
    except ValueError:
        return None


def _split_commands(tokens: list[str]) -> list[tuple[list[str], int]]:
    """Simple commands between shell operators, each with the index after its last word."""
    commands: list[tuple[list[str], int]] = []
    words: list[str] = []
    for index, token in enumerate(tokens):
        if _OPERATOR.fullmatch(token):
            if words:
                commands.append((words, index))
            words = []
        else:
            words.append(token)
    if words:
        commands.append((words, len(tokens)))
    return commands


def _go_test(words: list[str], rest_tokens: list[str], cwd: str) -> GoTest:
    flags: dict[str, str] = {}
    packages: list[str] = []
    index = 0
    while index < len(words):
        token = words[index]
        index += 1
        if token == "-args":
            return GoTest(flags, tuple(packages), cwd, tuple(words[index - 1 :]))
        if not token.startswith("-"):
            packages.append(token)
            continue
        name = token.lstrip("-")
        value: str | None = None
        if "=" in name:
            name, value = name.split("=", 1)
        if value is None and name in VALUE_FLAGS:
            value = words[index] if index < len(words) else ""
            index += 1
        flags[name] = "true" if value is None else value
    directory = posixpath.normpath(posixpath.join(cwd, flags["C"])) if "C" in flags else cwd
    return GoTest(flags, tuple(packages), directory, tuple(rest_tokens))


def read_commands(lines: list[str]) -> tuple[Commands, list[str]]:
    """The go test runs and Go-suite runs of some shell lines."""
    result = Commands(lines=lines)
    problems: list[str] = []
    cwd = "."
    for line in lines:
        tokens = _tokens(line)
        if tokens is None:
            problems.append(f"unparsed shell line: {line.strip()}")
            continue
        for words, end in _split_commands(tokens):
            start = 0
            while start < len(words) and re.fullmatch(r"[A-Za-z_]\w*=.*", words[start]):
                start += 1
            words = words[start:]
            if not words:
                continue
            if words[0] == "cd" and len(words) == 2:
                cwd = posixpath.normpath(posixpath.join(cwd, words[1]))
            elif words[:2] == ["go", "test"]:
                result.go_tests.append(_go_test(words[2:], tokens[end:], cwd))
            elif any(word.endswith("scripts/run_tests.py") for word in words):
                only = "all"
                for index, word in enumerate(words):
                    if word == "--only" and index + 1 < len(words):
                        only = words[index + 1]
                    elif word.startswith("--only="):
                        only = word.split("=", 1)[1]
                if only in ("all", "go"):
                    result.runs_go_suite = True
            elif words[0] == "make" and MAKE_GO_TARGETS.intersection(words[1:]):
                result.runs_go_suite = True
    return result, problems


def _in_module(package: str, cwd: str) -> bool:
    """True when the package pattern, read from cwd, can match a module package."""
    if package.startswith("$"):
        return False
    if package == "all":
        return True
    recursive = package.endswith("/...") or package == "..."
    base = package[: -len("...")].rstrip("/") if recursive else package
    target = posixpath.normpath(posixpath.join(cwd, base or "."))
    if target == MODULE or target.startswith(MODULE + "/"):
        return True
    return recursive and (target == "." or MODULE.startswith(target + "/"))


def devnet_broadcaster_tests() -> list[str]:
    """The planned broadcaster devnet tests plus any found in chain/devnet."""
    names = set(SHARD_RUN.values())
    pattern = re.compile(r"^func (TestDevnetBroadcaster\w*)\(t \*testing\.T\)", re.MULTILINE)
    for path in sorted((ROOT / DEVNET).glob("*_test.go")):
        names.update(pattern.findall(path.read_text(encoding="utf-8")))
    return sorted(names)


def _selects(test: GoTest, name: str) -> bool:
    """Whether the go test's -run and -skip filters select top-level test `name`."""
    for flag, wanted in (("run", True), ("skip", False)):
        if flag not in test.flags:
            continue
        expression = test.flags[flag].split("/", 1)[0]
        try:
            hit = re.search(expression, name) is not None
        except re.error:
            return True
        if hit != wanted:
            return False
    return True


def needs_postgres(test: GoTest, devnet_names: list[str]) -> bool:
    """True when the go test runs module tests or a broadcaster devnet test."""
    if any(_in_module(package, test.cwd) for package in test.packages):
        return True
    devnet = any(
        posixpath.normpath(posixpath.join(test.cwd, package)) == DEVNET
        for package in test.packages
        if not package.startswith("$")
    )
    return devnet and any(_selects(test, name) for name in devnet_names)


def _duration_seconds(value: str) -> int | None:
    match = _DURATION.fullmatch(value)
    if match is None or not value:
        return None
    hours, minutes, seconds = (int(part) if part else 0 for part in match.groups())
    return hours * 3600 + minutes * 60 + seconds


def pins() -> tuple[str, str]:
    """The PostgreSQL package version and apt key fingerprint of scripts/env/install.sh."""
    install = (ROOT / "scripts" / "env" / "install.sh").read_text(encoding="utf-8")
    version = re.search(r'^PG_VER="([^"]+)"$', install, re.MULTILINE)
    fingerprint = re.search(r'^PG_FPR="([0-9A-F]{40})"$', install, re.MULTILINE)
    assert version is not None and fingerprint is not None
    return version.group(1), fingerprint.group(1)


def install_problems(step: Mapping[str, Any], version: str, fingerprint: str) -> list[str]:
    """Why a step whose script names postgresql-18 is not the pinned install."""
    run = step.get("run")
    script = run if isinstance(run, str) else ""
    lines = [line.strip() for line in script.splitlines()]
    problems: list[str] = []
    if not lines or lines[0] != "set -euo pipefail":
        problems.append("does not start with set -euo pipefail")
    required = (
        f'pg_ver="{version}"',
        f'pg_fpr="{fingerprint}"',
        'sudo curl -fsSL -o "${key}" https://www.postgresql.org/media/keys/ACCC4CF8.asc',
        "gpg --batch --show-keys --with-colons",
        '"postgresql-18=${pg_ver}" "postgresql-client-18=${pg_ver}"',
        "installed=\"$(dpkg-query -W -f '${Version}' postgresql-18)\"",
        "/usr/lib/postgresql/18/bin/postgres --version",
    )
    for fragment in required:
        if fragment not in script:
            problems.append(f"does not contain {fragment!r}")
    for check in (
        'if [ "${fpr}" != "${pg_fpr}" ]; then',
        'if [ "${installed}" != "${pg_ver}" ]; then',
    ):
        if check not in lines:
            problems.append(f"has no check {check!r}")
            continue
        after = lines[lines.index(check) + 1 :]
        body = after[: after.index("fi")] if "fi" in after else []
        if "exit 1" not in body:
            problems.append(f"check {check!r} does not exit 1")
    if re.search(r"\|\|\s*(?:true|:)|\bset\s+\+\w*e|\btrap\b", script):
        problems.append("can hide a failure (|| true, set +e or trap)")
    for key in ("continue-on-error", "shell", "env"):
        if key in step:
            problems.append(f"sets {key}")
    return problems


def _install_shards(step: Mapping[str, Any]) -> tuple[list[str] | None, str | None]:
    """The shards an install step runs for (None: every shard), or a problem."""
    condition = step.get("if")
    if condition is None:
        return None, None
    match = _IF_SHARDS.fullmatch(str(condition).strip())
    if match is None:
        return [], f"unrecognized if condition {condition!r}"
    try:
        shards = json.loads(match.group(1))
    except json.JSONDecodeError:
        return [], f"unrecognized if condition {condition!r}"
    if not isinstance(shards, list) or not all(isinstance(item, str) for item in shards):
        return [], f"unrecognized if condition {condition!r}"
    return shards, None


def _shard_list(job: Mapping[str, Any]) -> list[str] | None:
    matrix = _mapping(_mapping(job.get("strategy")).get("matrix"))
    shards = matrix.get("shard")
    return [str(shard) for shard in shards] if isinstance(shards, list) else None


def _uses_shard(step: Mapping[str, Any]) -> bool:
    env = _mapping(step.get("env"))
    return str(env.get("SHARD", "")).replace(" ", "") == "${{matrix.shard}}"


def skip_variable_names(text: str) -> list[str]:
    """Identifiers that read as a PostgreSQL skip switch (SKIP and PG words)."""
    found: set[str] = set()
    for name in _IDENTIFIER.findall(text):
        words = set(name.upper().split("_"))
        if words & _SKIP_WORDS and words & _PG_WORDS:
            found.add(name)
    return sorted(found)


def go_skip_errors(files: Mapping[str, str]) -> list[str]:
    """Skip variables read by Go tests, and skips after a PostgreSQL or env check."""
    errors: list[str] = []
    for path, text in sorted(files.items()):
        for name in _ENV_READ.findall(text):
            if skip_variable_names(name):
                errors.append(f"{path} reads the PostgreSQL skip variable {name}")
        if path.startswith(MODULE + "/"):
            cause = _MODULE_SKIP_CAUSE
        elif path.startswith(DEVNET + "/"):
            cause = _DEVNET_SKIP_CAUSE
        else:
            continue
        lines = text.splitlines()
        for number, line in enumerate(lines):
            if not _SKIP_CALL.search(line):
                continue
            window = "\n".join(lines[max(0, number - 2) : number + 1])
            if cause.search(window):
                errors.append(
                    f"{path}:{number + 1} skips after a PostgreSQL, binary or environment check"
                )
    return errors


def go_test_files() -> dict[str, str]:
    """The services/broadcaster Go files and the chain/devnet test files."""
    files: dict[str, str] = {}
    for path in sorted((ROOT / MODULE).rglob("*.go")):
        files[path.relative_to(ROOT).as_posix()] = path.read_text(encoding="utf-8")
    for path in sorted((ROOT / DEVNET).glob("*_test.go")):
        files[path.relative_to(ROOT).as_posix()] = path.read_text(encoding="utf-8")
    return files


def _shard_errors(job: Mapping[str, Any]) -> list[str]:
    """The fixed checks of the three broadcaster shards of the go-race job."""
    errors: list[str] = []
    shards = _shard_list(job) or []
    race_steps = [step for step in _steps(job) if _uses_shard(step)]
    if len(race_steps) != 1:
        return [f"{RACE_JOB} has {len(race_steps)} steps with SHARD from matrix.shard"]
    run = race_steps[0].get("run")
    script = run if isinstance(run, str) else ""
    for shard in (MODULE_SHARD, *SHARD_RUN):
        if shard not in shards:
            errors.append(f"{RACE_JOB} matrix has no {shard} shard")
            continue
        lines, _ = shard_lines(script, shard)
        commands, _ = read_commands(lines)
        if len(commands.go_tests) != 1:
            errors.append(f"{RACE_JOB} shard {shard}: {len(commands.go_tests)} go test runs")
            continue
        test = commands.go_tests[0]
        if test.flags.get("race") not in ("true", "1"):
            errors.append(f"{RACE_JOB} shard {shard}: no -race")
        if test.flags.get("count") != "1":
            errors.append(f"{RACE_JOB} shard {shard}: -count is not 1")
        if test.tail:
            errors.append(f"{RACE_JOB} shard {shard}: shell operator after go test")
        if "timeout" not in test.flags:
            errors.append(f"{RACE_JOB} shard {shard}: no -timeout")
        else:
            seconds = _duration_seconds(test.flags["timeout"])
            if seconds is None or not 0 < seconds <= MAX_TIMEOUT_SECONDS:
                errors.append(
                    f"{RACE_JOB} shard {shard}: -timeout {test.flags['timeout']} "
                    f"is not between 1s and {MAX_TIMEOUT_SECONDS // 60}m"
                )
        if shard == MODULE_SHARD:
            if test.packages != ("./services/broadcaster/...",):
                errors.append(f"{RACE_JOB} shard {shard}: packages {list(test.packages)}")
            continue
        if test.packages != ("./chain/devnet",) or test.flags.get("run") != SHARD_RUN[shard]:
            errors.append(
                f"{RACE_JOB} shard {shard}: does not run -run {SHARD_RUN[shard]} ./chain/devnet"
            )
        normalized = [" ".join(line.split()) for line in lines]
        for prebuild in PREBUILDS:
            if prebuild not in normalized:
                errors.append(f"{RACE_JOB} shard {shard}: no prebuild {prebuild!r}")
    return errors


def ci_errors(data: Mapping[str, Any], text: str, sources: Mapping[str, str]) -> list[str]:
    """Every guard error for one workflow document; `sources` are other scanned texts."""
    errors: list[str] = []
    version, fingerprint = pins()
    jobs = _mapping(data.get("jobs"))
    devnet_names = devnet_broadcaster_tests()
    install_runs: set[str] = set()
    for job_name, raw in jobs.items():
        job = _mapping(raw)
        matrix = _shard_list(job)
        shards: list[str | None] = list(matrix) if matrix else [None]
        installs: list[tuple[int, list[str] | None]] = []
        for index, step in enumerate(_steps(job)):
            run = step.get("run")
            if not isinstance(run, str) or "postgresql-18" not in run:
                continue
            problems = install_problems(step, version, fingerprint)
            covered, condition_problem = _install_shards(step)
            if condition_problem is not None:
                problems.append(condition_problem)
            if covered is not None and job_name == GO_JOB:
                problems.append("has a condition in the go job")
            for problem in problems:
                errors.append(f"job {job_name}: PostgreSQL install step {problem}")
            if not problems:
                installs.append((index, covered))
                install_runs.add(run)
        for shard in shards:
            label = f"job {job_name}" + (f" shard {shard}" if shard is not None else "")
            for index, step in enumerate(_steps(job)):
                run = step.get("run")
                if not isinstance(run, str):
                    continue
                lines, problems = shard_lines(run, shard if _uses_shard(step) else None)
                errors.extend(f"{label}: case statement: {problem}" for problem in problems)
                commands, problems = read_commands(lines)
                errors.extend(f"{label}: {problem}" for problem in problems)
                needs = commands.runs_go_suite and MODULE in GO_MODULES
                needs = needs or any(
                    needs_postgres(test, devnet_names) for test in commands.go_tests
                )
                if not needs:
                    continue
                if not any(
                    position < index and (covered is None or shard in covered)
                    for position, covered in installs
                ):
                    errors.append(
                        f"{label}: step {step.get('name')!r} runs the services/broadcaster "
                        "tests without an earlier pinned PostgreSQL install"
                    )
    if len(install_runs) > 1:
        errors.append("the PostgreSQL install steps differ")
    go_job = _mapping(jobs.get(GO_JOB))
    go_lines = {
        line.strip()
        for step in _steps(go_job)
        if "run" in step
        for line in str(step["run"]).split("\n")
    }
    for line in GO_JOB_LINES:
        if line not in go_lines:
            errors.append(f"job go does not run {line}")
    errors.extend(_shard_errors(_mapping(jobs.get(RACE_JOB))))
    if _PHASES.search(text):
        errors.append("ci.yml names SVN_BROADCASTER_PHASES (devnet phase selection)")
    for path, source in {"ci.yml": text, **sources}.items():
        for name in skip_variable_names(source):
            errors.append(f"{path} names the PostgreSQL skip variable {name}")
    return errors


def _real_text() -> str:
    return CI.read_text(encoding="utf-8")


def _sources() -> dict[str, str]:
    return {
        "Makefile": (ROOT / "Makefile").read_text(encoding="utf-8"),
        "scripts/run_tests.py": (ROOT / "scripts" / "run_tests.py").read_text(encoding="utf-8"),
    }


def _errors_for_text(text: str) -> list[str]:
    data = yaml.safe_load(text)
    assert isinstance(data, dict)
    return ci_errors(data, text, _sources())


def test_real_workflow_installs_postgres_where_the_broadcaster_tests_run() -> None:
    assert _errors_for_text(_real_text()) == []


def test_pins_match_the_dependency_record() -> None:
    version, fingerprint = pins()
    dependencies = (ROOT / "docs" / "dependencies.md").read_text(encoding="utf-8")
    [row] = [line for line in dependencies.splitlines() if line.startswith("| PostgreSQL |")]
    assert f"package `{version}`" in row
    assert f"fingerprint is `{fingerprint}`" in row


def test_real_workflow_runs_the_module_in_the_go_job_and_three_shards() -> None:
    data = yaml.safe_load(_real_text())
    assert MODULE in GO_MODULES
    go_job = data["jobs"][GO_JOB]
    [test_step] = [s for s in _steps(go_job) if s.get("name") == "Format, generate, and test"]
    commands, problems = read_commands(_logical_lines(test_step["run"]))
    assert problems == [] and commands.runs_go_suite
    race = data["jobs"][RACE_JOB]
    [install] = [s for s in _steps(race) if "postgresql-18" in str(s.get("run"))]
    shards, problem = _install_shards(install)
    assert problem is None and shards is not None
    assert set((MODULE_SHARD, *SHARD_RUN)) <= set(shards)


Mutator = Callable[[dict[str, Any]], None]


def _step(data: dict[str, Any], job: str, predicate: Callable[[dict[str, Any]], bool]) -> int:
    steps = data["jobs"][job]["steps"]
    [index] = [i for i, step in enumerate(steps) if predicate(step)]
    return index


def _is_install(step: dict[str, Any]) -> bool:
    return "postgresql-18" in str(step.get("run"))


def _is_test(step: dict[str, Any]) -> bool:
    return step.get("name") == "Format, generate, and test"


def _drop_go_install(data: dict[str, Any]) -> None:
    data["jobs"][GO_JOB]["steps"].pop(_step(data, GO_JOB, _is_install))


def _move_go_install_after_tests(data: dict[str, Any]) -> None:
    steps = data["jobs"][GO_JOB]["steps"]
    steps.append(steps.pop(_step(data, GO_JOB, _is_install)))


def _drop_race_install(data: dict[str, Any]) -> None:
    data["jobs"][RACE_JOB]["steps"].pop(_step(data, RACE_JOB, _is_install))


def _edit_race_install(key: str, value: object) -> Mutator:
    def mutate(data: dict[str, Any]) -> None:
        data["jobs"][RACE_JOB]["steps"][_step(data, RACE_JOB, _is_install)][key] = value

    return mutate


def _edit_install_run(job: str, old: str, new: str) -> Mutator:
    def mutate(data: dict[str, Any]) -> None:
        step = data["jobs"][job]["steps"][_step(data, job, _is_install)]
        assert str(step["run"]).count(old) == 1, old
        step["run"] = str(step["run"]).replace(old, new)

    return mutate


def _edit_race_run(old: str, new: str) -> Mutator:
    def mutate(data: dict[str, Any]) -> None:
        step = data["jobs"][RACE_JOB]["steps"][_step(data, RACE_JOB, _uses_shard)]
        assert str(step["run"]).count(old) == 1, old
        step["run"] = str(step["run"]).replace(old, new)

    return mutate


def _edit_go_run(old: str, new: str) -> Mutator:
    def mutate(data: dict[str, Any]) -> None:
        step = data["jobs"][GO_JOB]["steps"][_step(data, GO_JOB, _is_test)]
        assert str(step["run"]).count(old) == 1, old
        step["run"] = str(step["run"]).replace(old, new)

    return mutate


def _drop_shard(shard: str) -> Mutator:
    def mutate(data: dict[str, Any]) -> None:
        data["jobs"][RACE_JOB]["strategy"]["matrix"]["shard"].remove(shard)

    return mutate


def _add_job(script: str) -> Mutator:
    def mutate(data: dict[str, Any]) -> None:
        data["jobs"]["extra"] = {
            "runs-on": "ubuntu-24.04",
            "timeout-minutes": 10,
            "steps": [{"name": "Extra tests", "run": script}],
        }

    return mutate


ALL_SHARDS = '["broadcaster", "devnet-broadcaster", "devnet-broadcaster-stall"]'
SKIP_UNION = (
    "TestDevnetAudit|TestDevnetUpgrade|TestDevnetMinority|TestDevnetGovernedChange"
    "|TestDevnetBroadcasterLifecycle|TestDevnetBroadcasterQuorumStall"
)
FPR_CHECK = 'if [ "${fpr}" != "${pg_fpr}" ]; then'

DATA_MUTATIONS = [
    pytest.param(
        _drop_go_install,
        "job go: step 'Format, generate, and test' runs the services/broadcaster tests "
        "without an earlier pinned PostgreSQL install",
        id="go-job-install-removed",
    ),
    pytest.param(
        _move_go_install_after_tests,
        "job go: step 'Format, generate, and test' runs the services/broadcaster tests",
        id="go-job-install-after-tests",
    ),
    pytest.param(
        _drop_race_install,
        "job go-race shard devnet-broadcaster-stall: step 'Race tests' runs",
        id="race-install-removed",
    ),
    pytest.param(
        _edit_race_install("if", ALL_SHARDS.replace(', "devnet-broadcaster-stall"', "")),
        "unrecognized if condition",
        id="race-install-if-not-the-contains-form",
    ),
    pytest.param(
        _edit_race_install(
            "if",
            "contains(fromJSON('"
            + ALL_SHARDS.replace(', "devnet-broadcaster-stall"', "")
            + "'), matrix.shard)",
        ),
        "job go-race shard devnet-broadcaster-stall: step 'Race tests' runs",
        id="race-install-misses-the-stall-shard",
    ),
    pytest.param(
        _edit_race_install("if", "contains(fromJSON('" + ALL_SHARDS + "'), matrix.os)"),
        "unrecognized if condition",
        id="race-install-if-on-another-key",
    ),
    pytest.param(
        _edit_race_install("continue-on-error", True),
        "PostgreSQL install step sets continue-on-error",
        id="race-install-continue-on-error",
    ),
    pytest.param(
        _edit_race_install("shell", "bash {0}"),
        "PostgreSQL install step sets shell",
        id="race-install-shell",
    ),
    pytest.param(
        _edit_install_run(GO_JOB, 'pg_ver="18.6-1.pgdg24.04+2"', 'pg_ver="18.5-1.pgdg24.04+1"'),
        "does not contain 'pg_ver=\"18.6-1.pgdg24.04+2\"'",
        id="go-install-other-version",
    ),
    pytest.param(
        _edit_install_run(GO_JOB, "B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8", "0" * 40),
        "does not contain 'pg_fpr=",
        id="go-install-other-fingerprint",
    ),
    pytest.param(
        _edit_install_run(RACE_JOB, FPR_CHECK, 'if [ "${fpr}" = "never" ]; then'),
        "has no check",
        id="race-install-fingerprint-check-removed",
    ),
    pytest.param(
        _edit_install_run(
            RACE_JOB,
            '"postgresql-18=${pg_ver}" "postgresql-client-18=${pg_ver}"',
            '"postgresql-18=${pg_ver}" "postgresql-client-18=${pg_ver}" || true',
        ),
        "can hide a failure",
        id="race-install-or-true",
    ),
    pytest.param(
        _edit_install_run(GO_JOB, "set -euo pipefail\n", "set -uo pipefail\n"),
        "does not start with set -euo pipefail",
        id="go-install-no-errexit",
    ),
    pytest.param(
        _edit_install_run(GO_JOB, "/usr/lib/postgresql/18/bin/postgres --version\n", ""),
        "does not contain '/usr/lib/postgresql/18/bin/postgres --version'",
        id="go-install-no-version-log",
    ),
    pytest.param(
        _edit_install_run(GO_JOB, "set -euo pipefail\n", "set -euo pipefail\necho copy\n"),
        "the PostgreSQL install steps differ",
        id="install-copies-differ",
    ),
    pytest.param(
        _edit_race_run(
            "go test -race -count=1 -timeout 12m ./services/broadcaster/...",
            "go test -race -count=1 -timeout 25m ./services/broadcaster/...",
        ),
        "shard broadcaster: -timeout 25m is not between 1s and 15m",
        id="module-shard-timeout-too-long",
    ),
    pytest.param(
        _edit_race_run(
            "go test -race -count=1 -timeout 12m ./services/broadcaster/...",
            "go test -count=1 -timeout 12m ./services/broadcaster/...",
        ),
        "shard broadcaster: no -race",
        id="module-shard-no-race",
    ),
    pytest.param(
        _edit_race_run(
            "go test -race -count=1 -timeout 12m ./services/broadcaster/...",
            "go test -race -count=0 -timeout 12m ./services/broadcaster/...",
        ),
        "shard broadcaster: -count is not 1",
        id="module-shard-count-zero",
    ),
    pytest.param(
        _edit_race_run(
            "go test -race -count=1 -timeout 15m -run TestDevnetBroadcasterLifecycle",
            "go test -race -count=1 -run TestDevnetBroadcasterLifecycle",
        ),
        "shard devnet-broadcaster: no -timeout",
        id="lifecycle-shard-no-timeout",
    ),
    pytest.param(
        _edit_race_run(
            "-run TestDevnetBroadcasterQuorumStall ./chain/devnet",
            "-run TestDevnetBroadcasterQuorumStall ./chain/devnet || true",
        ),
        "shard devnet-broadcaster-stall: shell operator after go test",
        id="stall-shard-or-true",
    ),
    pytest.param(
        _edit_race_run(
            "    # The same prebuilds as devnet-broadcaster, for\n"
            "    # TestDevnetBroadcasterQuorumStall.\n"
            "    go build -C chain -o /dev/null ./cmd/svnd\n"
            "    go build -C services/broadcaster -race -tags svn_faultpoint "
            "-o /dev/null ./cmd/svn-broadcaster\n",
            "",
        ),
        "shard devnet-broadcaster-stall: no prebuild",
        id="stall-shard-prebuilds-removed",
    ),
    pytest.param(
        _drop_shard("broadcaster"),
        "go-race matrix has no broadcaster shard",
        id="matrix-drops-module-shard",
    ),
    pytest.param(
        _edit_race_run(SKIP_UNION, SKIP_UNION.split("|TestDevnetBroadcaster", 1)[0]),
        "job go-race shard devnet: step 'Race tests' runs the services/broadcaster tests",
        id="devnet-shard-runs-broadcaster-tests-without-install",
    ),
    pytest.param(
        _edit_go_run("go vet ./services/broadcaster/...\n", ""),
        "job go does not run go vet ./services/broadcaster/...",
        id="go-job-no-vet",
    ),
    pytest.param(
        _edit_go_run("go vet -tags svn_faultpoint ./services/broadcaster/...\n", ""),
        "job go does not run go vet -tags svn_faultpoint ./services/broadcaster/...",
        id="go-job-no-faultpoint-vet",
    ),
    pytest.param(
        _edit_go_run("go vet -tags svn_roottest", "go vet"),
        "job go does not run go vet -tags svn_roottest ./services/broadcaster/internal/signer/ "
        "./services/broadcaster/internal/keys/",
        id="go-job-no-roottest-vet",
    ),
    pytest.param(
        _edit_go_run(
            " -tags svn_faultpoint ./services/broadcaster/internal/faultpoint/",
            " ./services/broadcaster/internal/faultpoint/",
        ),
        "job go does not run go test -count=1 -timeout 2m -tags svn_faultpoint "
        "./services/broadcaster/internal/faultpoint/",
        id="go-job-no-faultpoint-tests",
    ),
    pytest.param(
        _add_job("go test -race ./services/broadcaster/internal/store\n"),
        "job extra: step 'Extra tests' runs the services/broadcaster tests",
        id="new-job-runs-module-tests",
    ),
    pytest.param(
        _add_job("cd services/broadcaster\ngo test ./...\n"),
        "job extra: step 'Extra tests' runs",
        id="new-job-cd-into-module",
    ),
    pytest.param(
        _add_job("go test ./...\n"),
        "job extra: step 'Extra tests' runs",
        id="new-job-whole-workspace",
    ),
    pytest.param(
        _add_job("make test\n"),
        "job extra: step 'Extra tests' runs",
        id="new-job-make-test",
    ),
    pytest.param(
        _add_job(".venv/bin/python scripts/run_tests.py\n"),
        "job extra: step 'Extra tests' runs",
        id="new-job-runner-all-suites",
    ),
    pytest.param(
        _add_job("go test -race -run 'TestDevnetBroadcaster' ./chain/devnet\n"),
        "job extra: step 'Extra tests' runs",
        id="new-job-runs-broadcaster-devnet-tests",
    ),
]


@pytest.mark.parametrize(("mutate", "needle"), DATA_MUTATIONS)
def test_guard_fires_on_mutated_workflow(mutate: Mutator, needle: str) -> None:
    text = _real_text()
    data = copy.deepcopy(yaml.safe_load(text))
    mutate(data)
    errors = ci_errors(data, text, _sources())
    assert errors, needle
    assert any(needle in error for error in errors), errors


TEXT_MUTATIONS = [
    pytest.param(
        '          SVN_SKIP_DEVNET: "1"\n',
        '          SVN_SKIP_DEVNET: "1"\n          SVN_SKIP_POSTGRES: "1"\n',
        "ci.yml names the PostgreSQL skip variable SVN_SKIP_POSTGRES",
        id="go-step-env-skip-postgres",
    ),
    pytest.param(
        "permissions:\n  contents: read\n",
        'permissions:\n  contents: read\nenv:\n  PG_SKIP: "1"\n',
        "ci.yml names the PostgreSQL skip variable PG_SKIP",
        id="workflow-env-pg-skip",
    ),
    pytest.param(
        "          SHARD: ${{ matrix.shard }}\n",
        '          SHARD: ${{ matrix.shard }}\n          SVN_BROADCASTER_PHASES: "H"\n',
        "ci.yml names SVN_BROADCASTER_PHASES",
        id="race-step-env-phases",
    ),
    pytest.param(
        '          mod="github.com/Maxencejules/shortvideo-network"\n',
        "          export SVN_BROADCASTER_PHASES=TB1\n"
        '          mod="github.com/Maxencejules/shortvideo-network"\n',
        "ci.yml names SVN_BROADCASTER_PHASES",
        id="race-run-exports-phases",
    ),
    pytest.param(
        "          python3 scripts/run_tests.py --only go\n",
        "          SVN_NO_DB=1 python3 scripts/run_tests.py --only go\n",
        "ci.yml names the PostgreSQL skip variable SVN_NO_DB",
        id="go-run-inline-no-db",
    ),
]


@pytest.mark.parametrize(("old", "new", "needle"), TEXT_MUTATIONS)
def test_guard_fires_on_mutated_workflow_text(old: str, new: str, needle: str) -> None:
    text = _real_text()
    assert text.count(old) == 1, old
    errors = _errors_for_text(text.replace(old, new))
    assert any(needle in error for error in errors), errors


def test_guard_reads_skip_variables_in_the_makefile_and_runner() -> None:
    sources = _sources()
    sources["Makefile"] += "\nunit-go-fast:\n\tSKIP_POSTGRES=1 $(PY) scripts/run_tests.py\n"
    data = yaml.safe_load(_real_text())
    errors = ci_errors(data, _real_text(), sources)
    assert "Makefile names the PostgreSQL skip variable SKIP_POSTGRES" in errors, errors
    assert skip_variable_names("SVN_PGTEST_HELPER SVN_SKIP_DEVNET no_proxy") == []


def test_go_tests_have_no_postgres_skip() -> None:
    assert go_skip_errors(go_test_files()) == []


GO_SKIP_FIXTURES = [
    pytest.param(
        "services/broadcaster/internal/store/planted_test.go",
        'func TestX(t *testing.T) {\n\tif os.Getenv("SVN_SKIP_POSTGRES") == "1" {\n'
        '\t\tt.Skip("no database")\n\t}\n}\n',
        ["reads the PostgreSQL skip variable SVN_SKIP_POSTGRES", "skips after a PostgreSQL"],
        id="module-env-skip",
    ),
    pytest.param(
        "services/broadcaster/internal/store/planted_test.go",
        "func TestX(t *testing.T) {\n\tif _, err := os.Stat(pgtest.DefaultBinDir); err != nil {\n"
        '\t\tt.Skip("PostgreSQL 18 is not installed")\n\t}\n}\n',
        ["planted_test.go:3 skips after a PostgreSQL, binary or environment check"],
        id="module-missing-binary-skip",
    ),
    pytest.param(
        "services/broadcaster/internal/store/planted_test.go",
        'func TestX(t *testing.T) {\n\tif os.Getenv("CI") == "" {\n\t\tt.SkipNow()\n\t}\n}\n',
        ["planted_test.go:3 skips after a PostgreSQL, binary or environment check"],
        id="module-other-env-skip",
    ),
    pytest.param(
        "chain/devnet/broadcaster_test.go",
        'func TestX(t *testing.T) {\n\tif _, err := exec.LookPath("initdb"); err != nil {\n'
        '\t\tt.Skipf("no initdb: %v", err)\n\t}\n}\n',
        ["broadcaster_test.go:3 skips after a PostgreSQL, binary or environment check"],
        id="devnet-missing-initdb-skip",
    ),
    pytest.param(
        "chain/devnet/broadcaster_test.go",
        'func TestX(t *testing.T) {\n\tif os.LookupEnv("SKIP_PG") != "" {\n\t\treturn\n\t}\n}\n',
        ["reads the PostgreSQL skip variable SKIP_PG"],
        id="devnet-reads-skip-variable",
    ),
]


@pytest.mark.parametrize(("path", "text", "needles"), GO_SKIP_FIXTURES)
def test_guard_fires_on_planted_go_skips(path: str, text: str, needles: list[str]) -> None:
    errors = go_skip_errors({path: text})
    for needle in needles:
        assert any(needle in error for error in errors), (needle, errors)


def test_guard_allows_uid_skips_and_the_devnet_gate() -> None:
    files = {
        "services/broadcaster/internal/keys/planted_test.go": (
            'func TestX(t *testing.T) {\n\tif os.Geteuid() != 0 {\n\t\tt.Skip("needs euid 0")\n'
            "\t}\n}\n"
        ),
        "chain/devnet/broadcaster_test.go": (
            'func TestX(t *testing.T) {\n\tif os.Getenv("SVN_SKIP_DEVNET") == "1" {\n'
            '\t\tt.Skip("SVN_SKIP_DEVNET=1")\n\t}\n}\n'
        ),
    }
    assert go_skip_errors(files) == []


def test_parser_reads_cases_commands_and_filters() -> None:
    script = (
        'case "${SHARD}" in\n'
        "  a|b) go test -race \\\n"
        "      -run X ./chain/devnet ;;\n"
        "  c)\n"
        "    cd services/broadcaster && go test -C internal ./store\n"
        "    ;;\n"
        "esac\n"
    )
    lines, problems = shard_lines(script, "b")
    assert problems == []
    commands, problems = read_commands(lines)
    assert problems == [] and len(commands.go_tests) == 1
    assert commands.go_tests[0].flags == {"race": "true", "run": "X"}
    lines, _ = shard_lines(script, "c")
    commands, _ = read_commands(lines)
    [test] = commands.go_tests
    assert test.cwd == "services/broadcaster/internal" and needs_postgres(test, [])
    names = ["TestDevnetBroadcasterLifecycle", "TestDevnetBroadcasterQuorumStall"]
    run_audit = GoTest({"run": "TestDevnetAudit"}, ("./chain/devnet",), ".")
    assert not needs_postgres(run_audit, names)
    skip_both = GoTest(
        {"skip": "TestDevnetBroadcasterLifecycle|TestDevnetBroadcasterQuorumStall"},
        ("./chain/devnet",),
        ".",
    )
    assert not needs_postgres(skip_both, names)
    skip_one = GoTest({"skip": "TestDevnetBroadcasterLifecycle"}, ("./chain/devnet",), ".")
    assert needs_postgres(skip_one, names)
    assert not _in_module("${pkgs}", ".") and not _in_module("./chain/...", ".")
    assert _in_module("./services/...", ".") and _in_module("./...", ".")
    assert _duration_seconds("12m") == 720 and _duration_seconds("") is None


def test_scaffold_is_replaced_by_the_go_module() -> None:
    """The goal 03 Python scaffold, its test and its path pin are replaced (DR-11-20).

    Replacements: services/broadcaster/go.mod and cmd/svn-broadcaster/main.go in
    test_repo_policy.REQUIRED_PATHS, the module in scripts/run_tests.GO_MODULES and
    in `make build`, and the CI checks above.
    """
    module = ROOT / MODULE
    assert not (module / "svn_broadcaster").exists()
    assert sorted(path.name for path in module.rglob("*.py")) == []
    project = (ROOT / "pyproject.toml").read_text(encoding="utf-8")
    assert MODULE not in project
    go_work = (ROOT / "go.work").read_text(encoding="utf-8")
    assert re.search(r"^\s*\./services/broadcaster\s*$", go_work, re.MULTILINE)
    go_mod = (module / "go.mod").read_text(encoding="utf-8")
    assert go_mod.startswith(
        "module github.com/Maxencejules/shortvideo-network/services/broadcaster\n"
    )
    makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
    build = makefile.split("\nbuild:\n", 1)[1].split("\n\n", 1)[0]
    assert "\tgo build -C services/broadcaster ./...\n" in build + "\n"
