#!/usr/bin/env python3
"""Independent checks for the goal 00 companion pack.

Run from anywhere:

    python3 docs/evidence/goal-00/recheck.py

Exit 0 means the boolean assertions held. The blueprint diff is still
reported with diff's own exit code. A non-zero diff exit means the
normalized texts differ. This script does not relabel that as a match.
"""

from __future__ import annotations

import hashlib
import re
import subprocess
import sys
import tempfile
from collections import Counter
from difflib import SequenceMatcher
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
GOAL_RE = re.compile(r"^GOAL (\d{2}) / 30$", re.M)
EVIDENCE = ROOT / "docs" / "evidence" / "goal-00"
NORM_TXT = EVIDENCE / "blueprint.txt.norm"
NORM_MD = EVIDENCE / "blueprint.md.norm"
DIFF_PATH = EVIDENCE / "blueprint-normalized.diff"
WORD_RE = re.compile(r"[A-Za-z0-9]+(?:['’./:+#_\-][A-Za-z0-9]+)*")
HEADER_RE = re.compile(
    r"DECENTRALIZED SHORT-VIDEO NETWORK\s+PROJECT BLUEPRINT \| 12 OCT 2026"
)
# Cover title block, plus the seven table spans named by review.
TITLE_LINES = range(1, 11)
TABLE_RANGES = (
    (196, 209),
    (416, 429),
    (470, 485),
    (740, 749),
    (932, 950),
    (1094, 1103),
    (1242, 1260),
)

MUST_IGNORE = [
    ".env",
    "secrets/api.txt",
    "priv_validator_key.json",
    "chain/config/priv_validator_key.json",
    "node_key.json",
    "chain/config/node_key.json",
    "priv_validator_state.json",
    "keyring-test/keyhash.address",
    "keyring-file/operator.info",
    "operator-mnemonic.txt",
    "mnemonic.txt",
    "seed.txt",
    "seed.json",
    "seeds.txt",
    "seeds.json",
    "wallet-seed-phrase.txt",
    "wallet_seed_phrase.txt",
    "backup.sql",
    "dumps/backup.sql.gz",
    "data.dump",
    "app.sqlite",
    "app.sqlite3",
    "app.db",
    "id_rsa",
    "node.pem",
    "uploads/video.mp4",
    "media-uploads/clip.webm",
    "user-uploads/clip.mp4",
    "node_modules/left-pad/index.js",
    "dist/app.js",
    "build/out",
    "bin/chaind",
    "coverage/lcov.info",
    "run.log",
    ".cache/tmp",
    "__pycache__/mod.pyc",
]

MUST_TRACK = [
    ".env.example",
    "README.md",
    "AGENTS.md",
    "docs/blueprint.md",
    "docs/blueprint.txt",
    "docs/prompt-playbook.txt",
    "docs/goals/01-requirements-scope-and-threat-model.md",
    "docs/evidence/goal-00/recheck.py",
    "docs/progress.tsv",
    "x/accounts/mnemonic.go",
    "docs/mnemonic-recovery.md",
    "internal/build/version.go",
    "cmd/chaind/bin/x",
    "services/api/seed.ts",
    "apps/web/src/mnemonic.ts",
    "web/dist/app.js",
]

CODE_SUFFIXES = {
    ".go",
    ".ts",
    ".tsx",
    ".js",
    ".jsx",
    ".rs",
    ".java",
    ".sol",
    ".c",
    ".cc",
    ".cpp",
    ".h",
    ".hpp",
}
ALLOWED_PY = {
    "docs/evidence/goal-00/build_companion_pack.py",
    "docs/evidence/goal-00/recheck.py",
}
SECRET_RE = re.compile(
    r"-----BEGIN (?:OPENSSH |RSA |EC )?PRIVATE KEY-----|AKIA[0-9A-Z]{16}"
)
TOKEN_RE = re.compile(r"\b(token|coin|mining|sale)\b", re.IGNORECASE)
SPEC_PREFIXES = (
    "docs/blueprint.md",
    "docs/blueprint.txt",
    "docs/prompt-playbook.txt",
    "docs/prompt-playbook.pdf",
    "docs/blueprint.pdf",
    "docs/goals/",
    "AGENTS.md",
    "README.md",
    "docs/progress.md",
    "docs/progress.tsv",
    "docs/evidence/",
)


def run(cmd: list[str], cwd: Path = ROOT) -> subprocess.CompletedProcess[str]:
    print("+", " ".join(cmd))
    return subprocess.run(cmd, cwd=cwd, text=True, capture_output=True)


def normalize_whitespace(text: str) -> str:
    """Collapse horizontal whitespace, turn form feeds into newlines, drop blank lines."""
    text = text.replace("\f", "\n")
    lines = []
    for line in text.splitlines():
        collapsed = re.sub(r"[ \t]+", " ", line).strip()
        if collapsed:
            lines.append(collapsed)
    return "\n".join(lines) + "\n"


def stable_diff(stdout: str) -> str:
    lines = stdout.splitlines()
    if len(lines) >= 2:
        lines[0] = lines[0].split("\t", 1)[0]
        lines[1] = lines[1].split("\t", 1)[0]
    if not lines:
        return ""
    return "\n".join(lines) + "\n"


def check_goal_slices() -> int:
    print("== goal slices ==")
    playbook = (ROOT / "docs" / "prompt-playbook.txt").read_text()
    goals = sorted((ROOT / "docs" / "goals").glob("*.md"))
    print(f"goal_file_count {len(goals)}")
    matches = list(GOAL_RE.finditer(playbook))
    print(f"playbook_goal_headers {len(matches)}")
    if len(goals) != 30 or len(matches) != 30:
        print("slice_assert_exit 1")
        return 1
    required = [
        "Prerequisites:",
        "COPY PROMPT",
        "IMPLEMENTATION REQUIREMENTS",
        "ACCEPTANCE CHECKS",
        "REQUIRED HANDOFF",
    ]
    pieces = []
    for index, path in enumerate(goals):
        text = path.read_text()
        number = index + 1
        end = matches[index + 1].start() if number < 30 else playbook.find("\f", matches[index].start())
        expected = playbook[matches[index].start() : end]
        digest = hashlib.sha256(text.encode()).hexdigest()
        ok = (
            path.name.startswith(f"{number:02d}-")
            and text == expected
            and text.startswith(f"GOAL {number:02d} / 30\n")
            and text.strip() != ""
            and all(item in text for item in required)
            and GOAL_RE.findall(text) == [f"{number:02d}"]
        )
        print(f"{'OK' if ok else 'FAIL'} {path.name} bytes={len(text.encode())} sha256={digest}")
        if not ok:
            print("slice_assert_exit 1")
            return 1
        pieces.append(text)
    joined = "".join(pieces)
    expected_span = playbook[matches[0].start() : playbook.find("\f", matches[-1].start())]
    same = joined == expected_span
    print(f"joined_equals_playbook_span {same}")
    print(f"joined_bytes {len(joined.encode())}")
    print(
        "goal_slice_markdown_lists not_rewritten "
        "reason=exact byte slices; joining lines so numbered lists render would change the bytes"
    )
    code = 0 if same else 1
    print(f"slice_assert_exit {code}")
    return code


def contract_paragraphs(playbook: str) -> list[str]:
    start = playbook.find("Copy this contract into the root AGENTS.md")
    end_marker = "never end-to-end verification."
    end = playbook.find(end_marker, start)
    if start < 0 or end < 0:
        raise SystemExit("contract region missing from playbook")
    region = playbook[start : end + len(end_marker)]
    headings = {
        "Scope and authority",
        "Context and changes",
        "Cloud and credentials",
        "Implementation discipline",
        "Verification and status",
        "Handoff",
    }
    blocks: list[tuple[str, str]] = []
    current: list[str] = []

    def flush() -> None:
        if current:
            blocks.append(("p", " ".join(current)))
            current.clear()

    for line in region.splitlines():
        text = line.strip()
        if not text:
            flush()
            continue
        if text in headings or text.startswith("Project commands and schemas"):
            flush()
            if text in headings:
                blocks.append(("h", text))
                continue
        current.append(text)
    flush()
    return [text for _kind, text in blocks]


def check_agents() -> int:
    print("== AGENTS.md contract ==")
    playbook = (ROOT / "docs" / "prompt-playbook.txt").read_text()
    agents = (ROOT / "AGENTS.md").read_text()
    meta = "Copy this contract into the root AGENTS.md before running the goals."
    paragraphs = contract_paragraphs(playbook)
    if not paragraphs or not paragraphs[0].startswith(meta):
        print("contract_assert_exit 1 meta sentence missing from playbook extract")
        return 1
    expected = [paragraphs[0][len(meta) :].strip(), *paragraphs[1:]]
    body, _sep, model = agents.partition("## Model substitution")
    present = []
    missing = []
    for paragraph in expected:
        if paragraph in body:
            present.append(paragraph.split(".", 1)[0][:60])
        else:
            missing.append(paragraph[:80])
    print(f"contract_paragraphs_present {len(present)}")
    for item in present:
        print(f"present {item}")
    meta_in_agents = meta in agents
    print(f"meta_sentence_in_agents {meta_in_agents}")
    required_model = [
        "effort or reasoning setting actually used",
        "playbook's assigned model and effort",
        "Extra effort does not certify correctness.",
        "Never claim a model or an effort setting was used that was not.",
    ]
    model_missing = [item for item in required_model if item not in model]
    for item in required_model:
        print(f"model_phrase {'present' if item in model else 'MISSING'} {item}")
    # Sentence diff: one playbook sentence per line vs one AGENTS contract sentence per line.
    playbook_sentences = []
    for paragraph in paragraphs:
        playbook_sentences.extend(re.findall(r".+?[.](?=\s|$)", paragraph))
    agent_sentences = re.findall(r".+?[.](?=\s|$)", re.sub(r"\s+", " ", body))
    with tempfile.TemporaryDirectory() as tmp:
        left = Path(tmp) / "agents-playbook-sentences.txt"
        right = Path(tmp) / "agents-md-sentences.txt"
        left.write_text("".join(s + "\n" for s in playbook_sentences))
        right.write_text("".join(s + "\n" for s in agent_sentences))
        print(
            "+ diff -u --label playbook-contract-sentences "
            "--label agents-contract-sentences "
            "<extracted playbook sentences> <extracted AGENTS.md sentences>"
        )
        diff_proc = subprocess.run(
            [
                "diff",
                "-u",
                "--label",
                "playbook-contract-sentences",
                "--label",
                "agents-contract-sentences",
                str(left),
                str(right),
            ],
            text=True,
            capture_output=True,
        )
    print(stable_diff(diff_proc.stdout), end="")
    if diff_proc.stderr:
        print(diff_proc.stderr, end="")
    print(f"agents_contract_diff_exit {diff_proc.returncode}")
    only_meta = (
        not meta_in_agents
        and not missing
        and not model_missing
        and diff_proc.returncode == 1
        and meta in diff_proc.stdout
        and "Preserve stricter existing project instructions." in body
    )
    # The diff may also show heading-only lines that are not sentences. Assert
    # every non-meta playbook sentence is in AGENTS, which `missing` already covers.
    code = 0 if (not meta_in_agents and not missing and not model_missing) else 1
    print(f"contract_assert_exit {code}")
    print(f"agents_diff_shows_dropped_meta {meta in diff_proc.stdout}")
    if only_meta:
        print("agents_diff_note differences are the dropped meta sentence and markdown headings")
    return code


def check_ignore() -> int:
    print("== gitignore ==")
    failed = 0
    for path in MUST_IGNORE:
        proc = run(["git", "check-ignore", "-q", "--", path])
        print(f"ignore {path} exit={proc.returncode}")
        if proc.returncode != 0:
            failed += 1
    for path in MUST_TRACK:
        proc = run(["git", "check-ignore", "-q", "--", path])
        print(f"not_ignored {path} exit={proc.returncode}")
        if proc.returncode != 1:
            failed += 1
    code = 0 if failed == 0 else 1
    print(f"gitignore_assert_exit {code}")
    return code


def check_scan() -> int:
    print("== code, secrets, token/coin/sale/mining scan ==")
    listed = run(["git", "ls-files"])
    print(f"git_ls_files_exit {listed.returncode}")
    if listed.returncode != 0:
        print(listed.stderr)
        print("scan_assert_exit 1")
        return 1
    files = [line for line in listed.stdout.splitlines() if line]
    code_hits = []
    for rel in files:
        suffix = Path(rel).suffix.lower()
        if suffix in CODE_SUFFIXES or (suffix == ".py" and rel not in ALLOWED_PY):
            code_hits.append(rel)
    print(f"application_code_files {len(code_hits)}")
    for rel in code_hits:
        print(f"code {rel}")
    secret_hits = []
    token_outside = []
    token_inside_files: list[tuple[str, int]] = []
    for rel in files:
        path = ROOT / rel
        if path.suffix.lower() == ".pdf":
            continue
        try:
            text = path.read_text(errors="replace")
        except IsADirectoryError:
            continue
        if SECRET_RE.search(text):
            secret_hits.append(rel)
        matches = list(TOKEN_RE.finditer(text))
        if not matches:
            continue
        if rel.startswith(SPEC_PREFIXES):
            token_inside_files.append((rel, len(matches)))
        else:
            token_outside.append((rel, len(matches)))
    print(f"secret_files {len(secret_hits)}")
    for rel in secret_hits:
        print(f"secret {rel}")
    print(f"token_coin_mining_sale_files_in_spec_docs {len(token_inside_files)}")
    for rel, count in token_inside_files:
        print(f"spec {rel} count={count}")
    print(f"token_coin_mining_sale_files_outside_spec {len(token_outside)}")
    for rel, count in token_outside:
        print(f"outside {rel} count={count}")
    code = 0 if not code_hits and not secret_hits and not token_outside else 1
    print(f"scan_assert_exit {code}")
    return code


def reorder_line_allowed(line_no: int) -> bool:
    if line_no in TITLE_LINES:
        return True
    return any(start <= line_no <= end for start, end in TABLE_RANGES)


def blueprint_word_streams(source: str, markdown: str) -> tuple[list[tuple[str, int]], list[str]]:
    """Words with newline-only line numbers. Running headers and the transcription note are omitted."""
    markdown = re.sub(
        r"<!-- TRANSCRIPTION-NOTE-BEGIN -->.*?<!-- TRANSCRIPTION-NOTE-END -->\n?",
        "",
        markdown,
        flags=re.S,
    )
    numbered: list[tuple[str, int]] = []
    for line_no, line in enumerate(source.split("\n"), 1):
        if HEADER_RE.search(line):
            continue
        for match in WORD_RE.finditer(line):
            numbered.append((match.group(0).lower(), line_no))
    markdown_words = [match.group(0).lower() for match in WORD_RE.finditer(markdown)]
    return numbered, markdown_words


def check_blueprint_fidelity() -> int:
    """Exit 0 only when word lists match and every reorder stays in the allowed ranges."""
    print("== blueprint word-list fidelity ==")
    source = (ROOT / "docs" / "blueprint.txt").read_text()
    markdown = (ROOT / "docs" / "blueprint.md").read_text()
    numbered, markdown_words = blueprint_word_streams(source, markdown)
    source_words = [word for word, _line in numbered]
    print(f"word_count_txt {len(source_words)}")
    print(f"word_count_md {len(markdown_words)}")
    multiset_equal = Counter(source_words) == Counter(markdown_words)
    print(f"word_multiset_equal {multiset_equal}")
    print("word_tokenizer [A-Za-z0-9]+(?:['’./:+#_-][A-Za-z0-9]+)* lowercased")
    print("word_numbering newline-only; form feeds do not increment the line number")
    matcher = SequenceMatcher(a=source_words, b=markdown_words, autojunk=False)
    touched: set[int] = set()
    for tag, i1, i2, _j1, _j2 in matcher.get_opcodes():
        if tag == "equal":
            continue
        touched.update(numbered[index][1] for index in range(i1, i2))
    outside = sorted(line for line in touched if not reorder_line_allowed(line))
    print("allowed_title_lines 1-10")
    print("allowed_table_ranges " + " ".join(f"{start}-{end}" for start, end in TABLE_RANGES))
    print("reorder_source_lines " + " ".join(str(line) for line in sorted(touched)))
    print("reorder_lines_outside " + (" ".join(str(line) for line in outside) if outside else "none"))
    lines = source.split("\n")
    for line_no in outside:
        snippet = lines[line_no - 1].strip()[:120]
        print(f"outside_line {line_no} {snippet}")
    code = 0 if multiset_equal and not outside else 1
    print(f"blueprint_fidelity_exit {code}")
    if code != 0:
        print(
            "blueprint_fidelity_note word lists match as a multiset, "
            "but a reorder falls outside the allowed title and table ranges"
        )
    return code


def check_blueprint_diff() -> int:
    print("== blueprint normalized-whitespace diff ==")
    source = (ROOT / "docs" / "blueprint.txt").read_text()
    markdown = (ROOT / "docs" / "blueprint.md").read_text()
    NORM_TXT.write_text(normalize_whitespace(source))
    NORM_MD.write_text(normalize_whitespace(markdown))
    proc = run(
        [
            "diff",
            "-u",
            str(NORM_TXT.relative_to(ROOT)),
            str(NORM_MD.relative_to(ROOT)),
        ]
    )
    print(f"diff_exit {proc.returncode}")
    if proc.stderr:
        print(proc.stderr, end="")
    diff_text = stable_diff(proc.stdout)
    DIFF_PATH.write_text(diff_text)
    digest = hashlib.sha256(diff_text.encode()).hexdigest()
    print(f"diff_lines {diff_text.count(chr(10))}")
    print(f"diff_bytes {len(diff_text.encode())}")
    print(f"diff_sha256 {digest}")
    print(f"diff_file {DIFF_PATH.relative_to(ROOT)}")
    print("identity_claimed no")
    print("blueprint_diff_note exit 1 means the normalized texts differ; this is the logged diff, not a token-count pass")
    # The assertion is that diff ran and the artifact matches its stabilized stdout.
    code = 0 if proc.returncode in (0, 1) and DIFF_PATH.read_text() == diff_text else 1
    print(f"blueprint_diff_logged_exit {code}")
    return code


def check_progress(fidelity_exit: int) -> int:
    print("== goal 00 status ==")
    progress = (ROOT / "docs" / "progress.md").read_text()
    tsv = (ROOT / "docs" / "progress.tsv").read_text().splitlines()
    row = ""
    for line in progress.splitlines():
        if line.startswith("| 00 |"):
            row = line
            break
    print(f"progress_md_row {row}")
    data = [line for line in tsv if line and not line.startswith("goal\t")]
    print(f"progress_tsv_data_rows {len(data)}")
    for line in data:
        print(f"tsv {line}")
    allowed = {"IMPLEMENTED", "VERIFIED", "BLOCKED", "DEFERRED"}
    md_ok = any(f"| {status} |" in row for status in allowed)
    if "| VERIFIED |" in row and fidelity_exit != 0:
        print("progress_md_verified_rejected blueprint_fidelity_exit is not 0")
        md_ok = False
    tsv_ok = False
    for line in data:
        cols = line.split("\t")
        if cols and cols[0] == "00" and len(cols) > 1 and cols[1] in allowed:
            tsv_ok = True
            print(f"tsv_goal_00_status {cols[1]}")
            if len(cols) > 4:
                print(f"tsv_cloud_agent {cols[4]}")
            if cols[1] == "VERIFIED" and fidelity_exit != 0:
                print("verified_rejected blueprint_fidelity_exit is not 0")
                tsv_ok = False
            if len(cols) < 5 or cols[4] != "bc-1596bc25-19f9-57b8-bf0a-2fb414e326d7":
                print("tsv_cloud_agent_mismatch")
                tsv_ok = False
    code = 0 if md_ok and tsv_ok else 1
    print(f"progress_assert_exit {code}")
    return code


def main() -> None:
    results = {
        "slices": check_goal_slices(),
        "agents": check_agents(),
        "gitignore": check_ignore(),
        "scan": check_scan(),
        "blueprint_diff_logged": check_blueprint_diff(),
        "blueprint_fidelity": check_blueprint_fidelity(),
        "progress": 0,
    }
    results["progress"] = check_progress(results["blueprint_fidelity"])
    print("== summary ==")
    for name, code in results.items():
        print(f"{name} {code}")
    failed = [name for name, code in results.items() if code != 0]
    if failed:
        print(f"recheck_exit 1 failed={','.join(failed)}")
        sys.exit(1)
    print("recheck_exit 0")


if __name__ == "__main__":
    main()
