"""Checks of the operator runbooks under docs/operations.

Each runbook opens with the development-procedure header line, links the
governance specification section it follows, cites only svnd commands and
flags that exist in the svnd command tree (read from the Go source of
chain/cmd/svnd, every build tag), cites only Go test names that exist, and
never states a forbidden recovery step outside a marked "Never" list. Every
bash block of the walked runbooks carries a walk:<id> tag that
scripts/devnet/walk-runbooks.py runs.
"""

from __future__ import annotations

import importlib.util
import re
import sys
from dataclasses import dataclass, field
from pathlib import Path
from types import ModuleType

import pytest

ROOT = Path(__file__).resolve().parents[2]
OPERATIONS = ROOT / "docs" / "operations"
SPEC = ROOT / "docs" / "architecture" / "governance-and-upgrades.md"
SVND_SOURCES = (ROOT / "chain" / "cmd" / "svnd", ROOT / "chain" / "cmd" / "svnd" / "cli")
# Packages outside chain/cmd/svnd whose commands a tagged svnd build adds, by
# the package name the svnd source calls them with.
COMMAND_PACKAGES = {"statecheck": ROOT / "chain" / "internal" / "statecheck"}
WALKER = ROOT / "scripts" / "devnet" / "walk-runbooks.py"

# The goal-10 runbooks. Each links the specification.
GOAL10_RUNBOOKS = (
    "council-changes.md",
    "release-preparation.md",
    "source-and-digest-review.md",
    "operator-installation.md",
    "planned-halt-and-restart.md",
    "failed-upgrade.md",
    "governance-authorities.md",
)
WALKED = (
    "docs/operations/operator-installation.md",
    "docs/operations/planned-halt-and-restart.md",
    "docs/operations/failed-upgrade.md",
)
HEADER_SENTENCES = ("This is not a production runbook.", "Public launch is disabled.")

NEVER_OPEN = "<!-- never-list -->"
NEVER_CLOSE = "<!-- /never-list -->"

# Forbidden anywhere in a runbook outside a marked never-list: the recovery
# steps the owner's Q2 ruling and the goal forbid.
FORBIDDEN_TEXT = {
    "unsafe reset": re.compile(r"unsafe[-_ ]?reset[-_ ]?all", re.IGNORECASE),
    "hard rollback": re.compile(r"rollback\s+--hard", re.IGNORECASE),
    "rollback command": re.compile(
        r"\b(?:svnd|cometbft|cmtd|tendermint|simd)\s+rollback\b", re.IGNORECASE
    ),
    "state reset command": re.compile(r"\breset[-_]state\b", re.IGNORECASE),
    "halt height or time": re.compile(r"halt[-_ ](?:height|time)", re.IGNORECASE),
    "skip upgrade": re.compile(r"(?:skip[-_ ]upgrades?|unsafe[-_]skip)", re.IGNORECASE),
    "validator key file": re.compile(r"priv_validator_key\.json", re.IGNORECASE),
}
# Forbidden inside any fenced code block of a runbook.
FORBIDDEN_CODE = {
    "recursive or forced delete": re.compile(r"\brm\s+-[a-zA-Z]*[rRf]"),
    "new genesis": re.compile(r"\bdevnet\s+init\b"),
    "genesis overwrite": re.compile(
        r"(?:>\s*\S*genesis\.json|\b(?:cp|mv|install|ln)\b[^\n|;&]*genesis\.json)"
    ),
    "signing state removed": re.compile(r"(?:\brm\b|>|\btruncate\b)[^\n]*priv_validator_state"),
}

FENCE = re.compile(r"^( {0,3})```(.*)$")
LINK = re.compile(r"\]\(([^)\s]+)\)")
CODE_SPAN = re.compile(r"`([^`\n]+)`")
TEST_NAME = re.compile(r"\b(Test[A-Z][A-Za-z0-9_]*)\b")
REPO_PATH = re.compile(
    r"^((?:docs|chain|scripts|tests|proto|agents|\.github)/[A-Za-z0-9_./@-]*[A-Za-z0-9_])"
    r"(?::([0-9][0-9,\-]*))?$"
)
SHELL_STOP = {"|", "||", "&&", ";", "&", ">", ">>", "2>", "2>&1", "<"}
# An svnd invocation: the word svnd, a variable that holds an svnd binary, or
# a quoted path whose last element starts with svnd.
SVND_CALL = re.compile(
    r"(?:(?<![\w./$-])svnd|\"\$(?:SVN_BIN|NEW_BIN|CHECK_BIN)\"|\"\$[A-Z_]+/svnd[\w-]*\")"
    r"((?:[ \t]+[^\s]+)*)"
)
WALKER_CALL = re.compile(r"walk-runbooks\.py((?:[ \t]+[^\s]+)*)")
# Cobra adds these to every command.
HELP = {"--help", "-h", "help"}
# A binary token starts a shell command when the text before it on its line
# is empty or ends with one of these.
COMMAND_POSITION = ("nohup", "$(", "|", "&&", ";", "if", "!", "then", "do", "{", "exec", "time")


@dataclass
class Command:
    """One cobra command: its flags and its subcommands."""

    name: str
    flags: set[str] = field(default_factory=set)
    children: dict[str, Command] = field(default_factory=dict)


@dataclass(frozen=True)
class Fenced:
    line: int
    info: str
    text: str


def runbooks() -> list[Path]:
    return sorted(path for path in OPERATIONS.glob("*.md") if path.name != "README.md")


def fenced_blocks(text: str) -> list[Fenced]:
    out: list[Fenced] = []
    lines = text.splitlines()
    index = 0
    while index < len(lines):
        opening = FENCE.match(lines[index])
        if opening is None:
            index += 1
            continue
        end = index + 1
        while end < len(lines):
            closing = FENCE.match(lines[end])
            if closing is not None and closing.group(2).strip() == "":
                break
            end += 1
        assert end < len(lines), f"code fence at line {index + 1} is not closed"
        out.append(Fenced(index + 1, opening.group(2).strip(), "\n".join(lines[index + 1 : end])))
        index = end + 1
    return out


def prose_of(text: str) -> str:
    """The text without its fenced code blocks."""
    kept: list[str] = []
    inside = False
    for line in text.splitlines():
        if FENCE.match(line):
            inside = not inside
            continue
        if not inside:
            kept.append(line)
    return "\n".join(kept)


def outside_never(text: str) -> tuple[str, list[str]]:
    """Split text into the part outside never-lists and the never-list bodies."""
    outside: list[str] = []
    lists: list[str] = []
    rest = text
    while NEVER_OPEN in rest:
        before, _, after = rest.partition(NEVER_OPEN)
        assert NEVER_CLOSE in after, "a never-list is not closed"
        body, _, rest = after.partition(NEVER_CLOSE)
        outside.append(before)
        lists.append(body)
    assert NEVER_CLOSE not in rest, "a never-list close marker has no open marker"
    outside.append(rest)
    return "\n".join(outside), lists


def slug(heading: str) -> str:
    """The GitHub anchor of a Markdown heading."""
    text = heading.strip().lower().replace("`", "")
    text = re.sub(r"[^\w\- ]", "", text)
    return text.replace(" ", "-")


def anchors(path: Path) -> set[str]:
    out: set[str] = set()
    for line in prose_of(path.read_text(encoding="utf-8")).splitlines():
        match = re.match(r"^#{1,6}\s+(.*)$", line)
        if match:
            out.add(slug(match.group(1)))
    return out


def split_args(text: str) -> list[str]:
    """Split a Go argument list at top-level commas."""
    parts: list[str] = []
    depth = 0
    current = ""
    for char in text:
        if char in "([{":
            depth += 1
        elif char in ")]}":
            depth -= 1
        if char == "," and depth == 0:
            parts.append(current.strip())
            current = ""
            continue
        current += char
    if current.strip():
        parts.append(current.strip())
    return parts


def call_arguments(body: str, start: int) -> str:
    """The text between the parenthesis at start and its match."""
    depth = 0
    for index in range(start, len(body)):
        if body[index] == "(":
            depth += 1
        elif body[index] == ")":
            depth -= 1
            if depth == 0:
                return body[start + 1 : index]
    raise AssertionError("unbalanced call in the svnd source")


def svnd_tree() -> Command:
    """The svnd command tree, read from every non-test Go file of chain/cmd/svnd.

    A function's command variables are the `x := &cobra.Command{Use: "name"`
    literals in it (or its returned literal); `x.Flags().<Type>Var(&v,
    "flag"` adds a flag; `x.AddCommand(...)` adds a variable of the same
    function or the command a `fnCmd(...)` call returns. The root is the
    command named svnd. Every build tag's files are read. When main takes
    `opts, cmds := fn(...)` and calls `root.AddCommand(cmds...)`, every
    `[]*cobra.Command{...}` literal in any definition of fn (one per build
    tag) adds its commands to the root; a `pkg.Fn(...)` element is read from
    the package of COMMAND_PACKAGES (the svn_statecheck build adds
    statecheck.Command this way).
    """
    functions: dict[str, str] = {}
    definitions: dict[str, list[str]] = {}
    sources = [(directory, "") for directory in SVND_SOURCES]
    sources += [(directory, name + ".") for name, directory in COMMAND_PACKAGES.items()]
    for directory, qualifier in sources:
        for path in sorted(directory.glob("*.go")):
            if path.name.endswith("_test.go"):
                continue
            source = path.read_text(encoding="utf-8")
            for match in re.finditer(r"^func (\w+)\(.*?(?=^func |\Z)", source, re.M | re.S):
                functions[qualifier + match.group(1)] = match.group(0)
                definitions.setdefault(qualifier + match.group(1), []).append(match.group(0))
    variables: dict[str, dict[str, Command]] = {}
    returned: dict[str, Command] = {}
    for name, body in functions.items():
        local: dict[str, Command] = {}
        for match in re.finditer(
            r"(\w+)\s*:?=\s*&cobra\.Command\{\s*Use:\s*\"([a-z][a-z-]*)", body
        ):
            local[match.group(1)] = Command(match.group(2))
        literal = re.search(r"return &cobra\.Command\{\s*Use:\s*\"([a-z][a-z-]*)", body)
        if literal:
            local["<return>"] = Command(literal.group(1))
        for match in re.finditer(r"(\w+)\.Flags\(\)\.\w+\(&\w+,\s*\"([a-z][a-z-]*)\"", body):
            if match.group(1) in local:
                local[match.group(1)].flags.add(match.group(2))
        variables[name] = local
        back = re.findall(r"\breturn (\w+)\s*$", body, re.M)
        if literal:
            returned[name] = local["<return>"]
        elif back and back[-1] in local:
            returned[name] = local[back[-1]]
    for name, body in functions.items():
        local = variables[name]
        for match in re.finditer(r"(\w+)\.AddCommand\(", body):
            parent = local.get(match.group(1))
            if parent is None:
                continue
            for arg in split_args(call_arguments(body, match.end() - 1)):
                call = re.match(r"(\w+)\(", arg)
                child = returned.get(call.group(1)) if call else local.get(arg)
                # An argument the parser cannot resolve (a spread slice, an
                # expression) adds nothing; citing such a command then fails.
                if child is not None:
                    parent.children[child.name] = child
    roots = [cmd for local in variables.values() for cmd in local.values() if cmd.name == "svnd"]
    assert len(roots) == 1, f"expected one svnd root command, found {len(roots)}"
    main = functions.get("main", "")
    spread = re.search(r"\w+,\s*(\w+)\s*:=\s*(\w+)\(", main)
    if spread and re.search(r"\.AddCommand\(" + spread.group(1) + r"\.\.\.\)", main):
        for body in definitions.get(spread.group(2), []):
            for literal in re.finditer(r"\[\]\*cobra\.Command\{([^{}]*)\}", body):
                for arg in split_args(literal.group(1)):
                    call = re.match(r"([\w.]+)\(", arg)
                    child = returned.get(call.group(1)) if call else None
                    if child is not None:
                        roots[0].children[child.name] = child
    return roots[0]


def svnd_calls(text: str) -> list[list[str]]:
    """The argument words of every svnd invocation in the code of text."""
    sources = [block.text for block in fenced_blocks(text)]
    sources += CODE_SPAN.findall(prose_of(text))
    calls: list[list[str]] = []
    for source in sources:
        for line in source.splitlines():
            for match in SVND_CALL.finditer(line):
                before = line[: match.start()].rstrip()
                if before and not before.endswith(COMMAND_POSITION):
                    continue
                words: list[str] = []
                for raw in match.group(1).split():
                    if raw in SHELL_STOP or raw.startswith((">", "2>", "|", ")")):
                        break
                    closing = re.fullmatch(r"(.*?)\)+[\"';]*", raw)
                    words.append(closing.group(1) if closing else raw)
                    if closing:
                        break
                if words:
                    calls.append(words)
    return calls


def call_problem(tree: Command, words: list[str]) -> str | None:
    """None when words name an svnd command and only flags it declares."""
    command = tree
    index = 0
    if words and words[0] in HELP:
        return None
    while index < len(words) and re.fullmatch(r"[a-z][a-z-]*", words[index]):
        child = command.children.get(words[index])
        if child is None:
            if command is tree or command.children:
                return f"svnd has no command {' '.join(words[: index + 1])}"
            break
        command = child
        index += 1
    if command is tree:
        return "svnd invocation without a subcommand"
    for word in words[index:]:
        if word in HELP:
            continue
        if word.startswith("--"):
            flag = word[2:].split("=", 1)[0]
            if flag not in command.flags:
                return f"svnd {command.name} has no flag --{flag}"
    return None


def load_walker() -> ModuleType:
    spec = importlib.util.spec_from_file_location("walk_runbooks", WALKER)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def go_test_names() -> set[str]:
    names: set[str] = set()
    for path in (ROOT / "chain").rglob("*_test.go"):
        names.update(re.findall(r"^func (Test[A-Za-z0-9_]*)\(", path.read_text("utf-8"), re.M))
    return names


def forbidden_problems(text: str) -> list[str]:
    """The forbidden steps text states outside a never-list, and never-list faults."""
    problems: list[str] = []
    outside, lists = outside_never(text)
    for label, pattern in FORBIDDEN_TEXT.items():
        for match in pattern.finditer(outside):
            problems.append(f"{label}: {match.group(0)!r} outside a never-list")
    for block in fenced_blocks(text):
        for label, pattern in FORBIDDEN_CODE.items():
            for match in pattern.finditer(block.text):
                problems.append(
                    f"{label} in the code block at line {block.line}: {match.group(0)!r}"
                )
    for body in lists:
        if "```" in body:
            problems.append("a never-list holds a code block")
        items = [line for line in body.splitlines() if line.strip()]
        if not items:
            problems.append("an empty never-list")
        for item in items:
            if not item.startswith("- Never "):
                problems.append(f"a never-list line does not start with '- Never ': {item!r}")
    return problems


def test_runbooks_exist_and_open_with_the_header() -> None:
    names = {path.name for path in runbooks()}
    missing = set(GOAL10_RUNBOOKS) - names
    assert not missing, f"missing runbooks: {sorted(missing)}"
    for path in runbooks():
        lines = path.read_text(encoding="utf-8").splitlines()
        assert lines[0].startswith("# "), f"{path.name}: no title"
        first = next(line for line in lines[1:] if line.strip())
        for sentence in HEADER_SENTENCES:
            assert sentence in first, f"{path.name}: the first paragraph lacks {sentence!r}"


def test_every_goal10_runbook_links_its_specification_section() -> None:
    spec_anchors = anchors(SPEC)
    for name in GOAL10_RUNBOOKS:
        text = (OPERATIONS / name).read_text(encoding="utf-8")
        targets = [
            link.partition("#")[2]
            for link in LINK.findall(text)
            if link.startswith("../architecture/governance-and-upgrades.md#")
        ]
        assert targets, f"{name}: no link to a section of governance-and-upgrades.md"
        for target in targets:
            assert target in spec_anchors, f"{name}: no specification section #{target}"


def test_links_resolve() -> None:
    for path in [*runbooks(), OPERATIONS / "README.md"]:
        for link in LINK.findall(path.read_text(encoding="utf-8")):
            if re.match(r"^[a-z]+://", link) or link.startswith("mailto:"):
                continue
            target, _, anchor = link.partition("#")
            resolved = (path.parent / target).resolve() if target else path
            assert resolved.exists(), f"{path.name}: broken link {link}"
            if anchor and resolved.suffix == ".md":
                assert anchor in anchors(resolved), f"{path.name}: no anchor in {link}"


def test_cited_repository_paths_exist() -> None:
    for path in [*runbooks(), OPERATIONS / "README.md"]:
        text = path.read_text(encoding="utf-8")
        for span in CODE_SPAN.findall(prose_of(text)):
            match = REPO_PATH.match(span)
            if match is None:
                continue
            cited = ROOT / match.group(1)
            assert cited.exists(), f"{path.name}: cited path {match.group(1)} does not exist"
            if match.group(2):
                count = len(cited.read_text(encoding="utf-8").splitlines())
                for number in re.findall(r"\d+", match.group(2)):
                    assert 1 <= int(number) <= count, f"{path.name}: {span} is beyond line {count}"


def test_svnd_command_tree_is_read_from_the_source() -> None:
    tree = svnd_tree()
    for command in ("version", "start", "genesis", "devnet", "status"):
        assert command in tree.children, f"the parser lost svnd {command}"
    assert "init" in tree.children["devnet"].children
    assert "validate" in tree.children["genesis"].children
    assert tree.children["start"].flags >= {"home"}
    assert tree.children["status"].flags >= {"rpc"}
    assert tree.children["devnet"].children["init"].flags >= {"home", "peers", "chain-id"}
    assert tree.children["genesis"].children["validate"].flags >= {"file"}
    assert "statecheck" in tree.children, "the parser lost svnd statecheck (svn_statecheck build)"
    assert tree.children["statecheck"].flags >= {"home", "height"}


@pytest.mark.parametrize(
    ("text", "problem"),
    [
        ("`svnd version`", None),
        ("`svnd --help`", None),
        ('```bash\nhelp="$("$NEW_BIN" --help)"\n```', None),
        ('```bash\nup="$("$SVN_BIN" status --rpc "$R")"\n```', None),
        ('```bash\n"$NEW_BIN" start --home "$NODE"\n```', None),
        ('```bash\n"$CHECK_BIN" statecheck --home "$NODE_COPY"\n```', None),
        (
            '```bash\n"$CHECK_BIN" statecheck --export x\n```',
            "svnd statecheck has no flag --export",
        ),
        ("`svnd devnet init --home x --peers loopback`", None),
        ("`svnd rollback`", "svnd has no command rollback"),
        ("`svnd tx authority propose`", "svnd has no command tx"),
        ('```bash\n"$SVN_BIN" start --halt-height 10\n```', "svnd start has no flag --halt-height"),
        ("`svnd devnet reset`", "svnd has no command devnet reset"),
    ],
)
def test_command_check_refuses_commands_svnd_lacks(text: str, problem: str | None) -> None:
    tree = svnd_tree()
    problems = [call_problem(tree, words) for words in svnd_calls(text)]
    assert problems, f"no svnd invocation found in {text!r}"
    assert problems[0] == problem


def test_cited_svnd_commands_exist() -> None:
    tree = svnd_tree()
    found = 0
    for path in [*runbooks(), OPERATIONS / "README.md"]:
        for words in svnd_calls(path.read_text(encoding="utf-8")):
            found += 1
            problem = call_problem(tree, words)
            assert problem is None, f"{path.name}: {problem} ({' '.join(words)})"
    assert found >= 10, f"only {found} svnd invocations found; the scan is broken"


def test_cited_walker_subcommands_exist() -> None:
    walker = load_walker()
    found = 0
    for path in [*runbooks(), OPERATIONS / "README.md"]:
        text = path.read_text(encoding="utf-8")
        sources = [block.text for block in fenced_blocks(text)] + CODE_SPAN.findall(prose_of(text))
        for source in sources:
            for match in WALKER_CALL.finditer(source):
                words = match.group(1).split()
                if not words:
                    continue
                found += 1
                assert words[0] in {"list", "walk", "query"}, f"{path.name}: {words[0]}"
                if words[0] == "query" and len(words) > 1:
                    assert words[1] in walker.QUERIES, f"{path.name}: query {words[1]}"
    assert found >= 3


def test_cited_go_tests_exist() -> None:
    names = go_test_names()
    assert "TestUpgradeRehearsalInProcess" in names, "the test name scan is broken"
    cited = 0
    for path in [*runbooks(), OPERATIONS / "README.md", SPEC]:
        for name in TEST_NAME.findall(path.read_text(encoding="utf-8")):
            cited += 1
            assert name in names, f"{path.name}: no Go test {name} under chain/"
    assert cited >= 20


def test_walked_runbooks_tag_every_bash_block() -> None:
    walker = load_walker()
    assert tuple(walker.WALKED) == WALKED
    blocks = walker.walk_blocks(ROOT)
    assert len({block.walk_id for block in blocks}) == len(blocks)
    for source in WALKED:
        text = (ROOT / source).read_text(encoding="utf-8")
        bash = [block for block in fenced_blocks(text) if block.info.split()[:1] == ["bash"]]
        assert bash, f"{source}: no bash block"
        for block in bash:
            assert re.fullmatch(r"bash walk:[a-z0-9][a-z0-9-]*", block.info), (
                f"{source}:{block.line}: bash block without a walk:<id> tag"
            )
        assert sum(1 for block in blocks if block.source == source) == len(bash)


def test_no_forbidden_step_outside_the_never_list() -> None:
    for path in [*runbooks(), OPERATIONS / "README.md"]:
        problems = forbidden_problems(path.read_text(encoding="utf-8"))
        assert not problems, f"{path.name}: {problems}"
    failed = (OPERATIONS / "failed-upgrade.md").read_text(encoding="utf-8")
    assert NEVER_OPEN in failed, "failed-upgrade.md lost its never-list"


@pytest.mark.parametrize(
    "inserted",
    [
        "Run `cometbft unsafe-reset-all --home $NODE` to recover.",
        '```bash walk:bad\nsvnd rollback --hard --home "$NODE"\n```',
        "Set `--halt-height` in the node configuration.",
        '```bash walk:bad\nrm -rf "$NODE/data"\n```',
        '```bash walk:bad\nsvnd devnet init --home "$SVN_HOME"\n```',
        "Copy `priv_validator_key.json` to the spare node.",
        "<!-- never-list -->\n- Run `unsafe-reset-all`.\n<!-- /never-list -->",
    ],
)
def test_forbidden_check_catches_an_inserted_step(inserted: str) -> None:
    text = (OPERATIONS / "failed-upgrade.md").read_text(encoding="utf-8")
    assert not forbidden_problems(text)
    mutated = text.replace("## Fix forward", inserted + "\n\n## Fix forward", 1)
    assert mutated != text
    assert forbidden_problems(mutated), f"the check missed {inserted!r}"


def test_release_runbook_names_the_release_build_checks() -> None:
    text = (OPERATIONS / "release-preparation.md").read_text(encoding="utf-8")
    for needle in ("upgrades none", "-tags", "vcs.revision", "vcs.modified", "no build tag"):
        assert needle in text, f"release-preparation.md lacks {needle!r}"
    failed = (OPERATIONS / "failed-upgrade.md").read_text(encoding="utf-8")
    for needle in (
        "Council plan, fail closed (Recommended)",
        "DR-10-11",
        "When rollback is unsafe",
    ):
        assert needle in failed, f"failed-upgrade.md lacks {needle!r}"
    halt = (OPERATIONS / "planned-halt-and-restart.md").read_text(encoding="utf-8")
    assert "DR-10-41" in halt
