#!/usr/bin/env python3
"""Regenerate or check the goal-00 companion pack from in-repo sources.

Default mode compares generated goals, blueprint markdown, AGENTS.md, and
.gitignore with the working tree and exits non-zero on a mismatch.
Pass --write to replace those generated files.

Sources are docs/prompt-playbook.txt and docs/blueprint.txt. Goal pages are
exact playbook slices. They are not reflowed into Markdown lists, because
joining lines would change the bytes.

The public blueprint comparison is the normalized-whitespace diff in
docs/evidence/goal-00/recheck.py. This generator still refuses to emit a
blueprint that drops source words, and it does not print a token-count pass.
"""

from __future__ import annotations

import argparse
import hashlib
import re
import sys
import tempfile
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
PLAYBOOK_TXT = ROOT / "docs" / "prompt-playbook.txt"
BLUEPRINT_TXT = ROOT / "docs" / "blueprint.txt"

TOKEN_RE = re.compile(r"[A-Za-z0-9]+(?:['’./:+#_\-][A-Za-z0-9]+)*")
GOAL_RE = re.compile(r"^GOAL (\d{2}) / 30$", re.M)
HEADER_RE = re.compile(
    r"^DECENTRALIZED SHORT-VIDEO NETWORK\s+PROJECT BLUEPRINT \| 12 OCT 2026\s*$"
)
FOOTER_RE = re.compile(r"^Proposed architecture / Version 1\.0\s+(\d+)\s*$")
TOC_RE = re.compile(r"^(\d{2}|[AS])\s+(.+?)\s{3,}(\d+)\s*$")
SECTION_RE = re.compile(r"^(?:\d{2}\s+\S.*|A Glossary and interpretation)$")
SOURCE_RE = re.compile(r"^S\d{2}\s+\S")
URL_RE = re.compile(r"https?://\S+")

NOTE_BEGIN = "<!-- TRANSCRIPTION-NOTE-BEGIN -->"
NOTE_END = "<!-- TRANSCRIPTION-NOTE-END -->"
NOTE = """\
> Lightly cleaned Markdown transcription of the extracted blueprint text.
> Repeated running headers are omitted. Each page keeps the footer line
> `Proposed architecture / Version 1.0` and its page number. Wording is
> unchanged. `docs/blueprint.pdf` is the source for ambiguous extraction,
> tables and diagrams.
"""


def tokens(text: str) -> list[str]:
    return [m.group(0).lower() for m in TOKEN_RE.finditer(text)]


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def slugify(title: str) -> str:
    slug = title.strip().lower().replace("&", " and ")
    slug = re.sub(r"[^a-z0-9]+", "-", slug).strip("-")
    return slug


def leading(line: str) -> int:
    return len(line) - len(line.lstrip(" "))


def is_category(line: str) -> bool:
    if line.startswith(" "):
        return False
    text = line.strip()
    if not text or len(text) > 40 or text != text.upper():
        return False
    if re.search(r"\d", text) or not re.search(r"[A-Z]", text):
        return False
    return True


def is_prose_callout(line: str) -> bool:
    if leading(line) != 3 or not line.strip():
        return False
    text = line.strip()
    if any(mark in text for mark in ("->", "{", "}", "=")):
        return False
    if text.endswith("/"):
        return False
    return text[:1].isalpha()


def is_pre_line(line: str) -> bool:
    if not line.strip():
        return False
    ind = leading(line)
    if ind >= 8:
        return True
    return ind >= 3 and not is_prose_callout(line)


def is_table_anchor(line: str) -> bool:
    return (
        line.startswith("  ")
        and not line.startswith("   ")
        and re.search(r"\S\s{3,}\S", line) is not None
    )


def column_starts(line: str) -> list[int]:
    starts: list[int] = []
    index = 0
    while index < len(line) and line[index] == " ":
        index += 1
    if index >= len(line):
        return starts
    starts.append(index)
    while index < len(line):
        match = re.search(r"\s{3,}\S", line[index:])
        if not match:
            break
        start = index + match.end() - 1
        starts.append(start)
        index = start
    return starts


def cells_from_line(line: str, starts: list[int]) -> list[list[str]]:
    cells: list[list[str]] = [[] for _ in starts]
    for match in re.finditer(r"\S+", line):
        column = 0
        for index, start in enumerate(starts):
            if match.start() + 1 >= start:
                column = index
        cells[column].append(match.group(0))
    return cells


def is_subsection(line: str, nxt: str | None) -> bool:
    if line[:1].isspace():
        return False
    text = line.strip()
    if not text or len(text) > 80 or "." in text:
        return False
    if not text[:1].isupper():
        return False
    if "  " in text or text.isupper() or not re.search(r"[a-z]", text):
        return False
    if SOURCE_RE.match(text) or TOC_RE.match(text) or SECTION_RE.match(text):
        return False
    if text.startswith("http://") or text.startswith("https://"):
        return False
    if nxt is not None:
        follower = nxt.strip()
        if follower[:1].islower():
            return False
    return True


def next_nonempty(lines: list[str], index: int) -> str | None:
    cursor = index + 1
    while cursor < len(lines) and not lines[cursor].strip():
        cursor += 1
    if cursor >= len(lines):
        return None
    return lines[cursor]


def parse_body(lines: list[str]) -> list[tuple]:
    blocks: list[tuple] = []
    index = 0
    total = len(lines)
    while index < total:
        line = lines[index]
        if not line.strip():
            index += 1
            continue
        if TOC_RE.match(line.strip()) and not line.startswith(" "):
            items = []
            while index < total and lines[index].strip() and TOC_RE.match(lines[index].strip()):
                match = TOC_RE.match(lines[index].strip())
                assert match is not None
                items.append((match.group(1), match.group(2).strip(), match.group(3)))
                index += 1
            blocks.append(("toc", items))
            continue
        if is_category(line):
            blocks.append(("category", line.strip()))
            index += 1
            continue
        if (
            SECTION_RE.match(line.strip())
            and not line.startswith(" ")
            and "." not in line
        ):
            blocks.append(("section", line.strip()))
            index += 1
            continue
        if SOURCE_RE.match(line.strip()) and not line.startswith(" "):
            title = line.strip()
            index += 1
            desc: list[str] = []
            url = None
            while index < total and lines[index].strip():
                current = lines[index].strip()
                if (
                    SOURCE_RE.match(current)
                    or is_category(lines[index])
                    or current.startswith("Proposed architecture")
                ):
                    break
                if current.startswith("http://") or current.startswith("https://"):
                    url = current
                    index += 1
                    break
                desc.append(current)
                index += 1
            blocks.append(("source", title, desc, url))
            continue
        if is_subsection(line, next_nonempty(lines, index)):
            blocks.append(("sub", line.strip()))
            index += 1
            continue
        if is_table_anchor(line):
            raw_rows: list[str] = []
            while index < total:
                if is_table_anchor(lines[index]):
                    raw_rows.append(lines[index])
                    index += 1
                    while (
                        index < total
                        and lines[index].strip()
                        and leading(lines[index]) > 2
                    ):
                        raw_rows.append(lines[index])
                        index += 1
                    continue
                if not lines[index].strip():
                    cursor = index
                    while cursor < total and not lines[cursor].strip():
                        cursor += 1
                    if cursor < total and is_table_anchor(lines[cursor]):
                        index = cursor
                        continue
                break
            blocks.append(("table", raw_rows))
            continue
        if is_pre_line(line):
            raw: list[str] = []
            while index < total:
                current = lines[index]
                if not current.strip():
                    cursor = index
                    while cursor < total and not lines[cursor].strip():
                        cursor += 1
                    if cursor < total and is_pre_line(lines[cursor]):
                        raw.extend(lines[index:cursor])
                        index = cursor
                        continue
                    break
                if is_pre_line(current) or (raw and leading(current) >= 3):
                    raw.append(current)
                    index += 1
                    continue
                break
            blocks.append(("pre", raw))
            continue
        if is_prose_callout(line):
            raw = [line]
            index += 1
            while index < total and is_prose_callout(lines[index]):
                raw.append(lines[index])
                index += 1
            blocks.append(("callout", raw))
            continue
        if line.startswith(" "):
            raise SystemExit(f"unclassified indented line: {line!r}")
        paragraph = [line.strip()]
        index += 1
        while index < total and lines[index].strip() and not lines[index].startswith(" "):
            if (
                is_category(lines[index])
                or (
                    SECTION_RE.match(lines[index].strip())
                    and "." not in lines[index]
                )
                or SOURCE_RE.match(lines[index].strip())
                or TOC_RE.match(lines[index].strip())
                or is_subsection(lines[index], next_nonempty(lines, index))
            ):
                break
            paragraph.append(lines[index].strip())
            index += 1
        blocks.append(("para", paragraph))
    return blocks


def is_wrapped_first_column(line: str) -> bool:
    match = re.search(r"\S+", line)
    if not match:
        return False
    return re.fullmatch(r"[a-z][a-z-]*", match.group(0)) is not None


def render_table(raw_rows: list[str]) -> str:
    anchors = [row for row in raw_rows if is_table_anchor(row)]
    if not anchors:
        raise SystemExit("table without an anchor row")
    starts = column_starts(anchors[0])
    if len(starts) < 2:
        raise SystemExit(f"table anchor has fewer than 2 columns: {anchors[0]!r}")
    logical: list[list[str]] = []
    current: list[list[str]] | None = None
    for row in raw_rows:
        if is_table_anchor(row):
            if current is not None and is_wrapped_first_column(row):
                extra = cells_from_line(row, starts)
                for index, words in enumerate(extra):
                    current[index].extend(words)
                continue
            if current is not None:
                logical.append([" ".join(cell) for cell in current])
            current = cells_from_line(row, starts)
            continue
        extra = cells_from_line(row, starts)
        if current is None:
            raise SystemExit(f"table continuation before anchor: {row!r}")
        for index, words in enumerate(extra):
            current[index].extend(words)
    if current is not None:
        logical.append([" ".join(cell) for cell in current])

    def esc(cell: str) -> str:
        return cell.replace("|", "\\|")

    width = len(starts)
    rendered = [
        "| " + " | ".join(esc(cell) for cell in logical[0]) + " |",
        "| " + " | ".join("---" for _ in range(width)) + " |",
    ]
    for row in logical[1:]:
        padded = row + [""] * (width - len(row))
        rendered.append("| " + " | ".join(esc(cell) for cell in padded[:width]) + " |")
    markdown = "\n".join(rendered)
    if Counter(tokens("\n".join(raw_rows))) != Counter(tokens(markdown)):
        raise SystemExit("table token mismatch:\n" + "\n".join(raw_rows[:8]))
    return markdown


def render_blocks(blocks: list[tuple]) -> str:
    parts: list[str] = []
    subsections: list[str] = []
    for block in blocks:
        kind = block[0]
        if kind == "category":
            parts.append(f"## {block[1]}")
        elif kind == "section":
            parts.append(f"### {block[1]}")
        elif kind == "sub":
            subsections.append(block[1])
            parts.append(f"#### {block[1]}")
        elif kind == "toc":
            items = []
            for _number, title, page in block[1]:
                items.append(f"- { _number } {title} ({page})")
            parts.append("\n".join(items))
        elif kind == "para":
            parts.append(" ".join(block[1]))
        elif kind == "callout":
            parts.append("> " + " ".join(line.strip() for line in block[1]))
        elif kind == "pre":
            body = "\n".join(line.rstrip() for line in block[1])
            parts.append("```\n" + body + "\n```")
        elif kind == "table":
            parts.append(render_table(block[1]))
        elif kind == "source":
            _kind, title, desc, url = block
            chunk = [f"#### {title}", "", " ".join(desc)]
            if url:
                chunk.extend(["", url])
            parts.append("\n".join(chunk))
        else:
            raise SystemExit(f"unknown block {kind}")
    return "\n\n".join(parts), subsections


def render_cover(page: str) -> str:
    lines = [line.strip() for line in page.splitlines() if line.strip()]
    expected = [
        "OPEN-SOURCE PRODUCT + CUSTOM BLOCKCHAIN",
        "Decentralized",
        "Short-Video",
        "Network",
        "Complete Project Summary",
        "& Blockchain Implementation Blueprint",
        "A familiar social-video app.",
        "Independently operated infrastructure.",
        "A purpose-built chain for shared hosting commitments.",
        "Prepared for Maxence Jules",
        "October 12, 2026 | Version 1.0",
        "Implementation proposal. Not a deployed system, audited protocol,",
        "investment offering or production performance claim.",
    ]
    if lines != expected:
        raise SystemExit(f"cover lines changed:\n{lines!r}")
    rendered = "\n".join(
        [
            "# Decentralized Short-Video Network",
            "",
            "Complete Project Summary & Blockchain Implementation Blueprint",
            "",
            "OPEN-SOURCE PRODUCT + CUSTOM BLOCKCHAIN",
            "",
            "A familiar social-video app.",
            "",
            "Independently operated infrastructure.",
            "",
            "A purpose-built chain for shared hosting commitments.",
            "",
            "Prepared for Maxence Jules",
            "",
            "October 12, 2026 | Version 1.0",
            "",
            "Implementation proposal. Not a deployed system, audited protocol, investment offering or production performance claim.",
        ]
    )
    if Counter(tokens("\n".join(lines))) != Counter(tokens(rendered)):
        raise SystemExit("cover token mismatch")
    return rendered


def build_blueprint(text: str) -> tuple[str, list[str]]:
    pages = text.split("\f")
    if pages[-1] == "":
        pages = pages[:-1]
    if len(pages) != 33:
        raise SystemExit(f"expected 33 blueprint pages, found {len(pages)}")
    subsections: list[str] = []
    chunks = [render_cover(pages[0]), "", NOTE_BEGIN, NOTE.rstrip(), NOTE_END, ""]
    body_for_check: list[str] = [
        line for line in pages[0].splitlines() if line.strip()
    ]
    for page in pages[1:]:
        footer_num = None
        body_lines: list[str] = []
        for line in page.splitlines():
            if HEADER_RE.match(line):
                continue
            footer = FOOTER_RE.match(line)
            if footer:
                footer_num = footer.group(1)
                continue
            body_lines.append(line)
        body_for_check.extend(body_lines)
        blocks = parse_body(body_lines)
        rendered, found = render_blocks(blocks)
        subsections.extend(found)
        chunks.append(rendered)
        chunks.append("")
        if footer_num is None:
            raise SystemExit("blueprint page missing footer")
        chunks.append(f"Proposed architecture / Version 1.0 {footer_num}")
        chunks.append("")
    markdown = "\n".join(chunks).rstrip() + "\n"
    return markdown, subsections, "\n".join(body_for_check)


def strip_note(markdown: str) -> str:
    begin = markdown.find(NOTE_BEGIN)
    end = markdown.find(NOTE_END)
    if begin < 0 or end < 0:
        raise SystemExit("transcription note markers missing")
    return markdown[:begin] + markdown[end + len(NOTE_END) :]


def check_blueprint(source_body: str, markdown: str, source_text: str) -> None:
    stripped = strip_note(markdown)
    source_tokens = Counter(tokens(source_body))
    # Footers were removed from source_body and re-emitted. Add them back by
    # counting footer lines in the original and requiring the markdown to
    # contain the same footer words. source_body excludes footers, and the
    # markdown includes one footer line per page, so compare body tokens to
    # markdown tokens minus footer lines.
    footer_lines = re.findall(
        r"^Proposed architecture / Version 1\.0\s+(\d+)\s*$",
        source_text,
        flags=re.M,
    )
    markdown_wo_footers = re.sub(
        r"^Proposed architecture / Version 1\.0 \d+\s*$",
        "",
        stripped,
        flags=re.M,
    )
    md_tokens = Counter(tokens(markdown_wo_footers))
    if source_tokens != md_tokens:
        missing = source_tokens - md_tokens
        extra = md_tokens - source_tokens
        raise SystemExit(
            "blueprint wording mismatch\n"
            f"missing {missing.most_common(20)}\n"
            f"extra {extra.most_common(20)}"
        )
    if len(footer_lines) != 32:
        raise SystemExit(f"expected 32 numbered pages, found {len(footer_lines)}")
    for number in footer_lines:
        if f"Proposed architecture / Version 1.0 {number}" not in markdown:
            raise SystemExit(f"missing footer for page {number}")
    for symbol in ("+", "×", "<=", ">=", "->", "*"):
        if source_body.count(symbol) != markdown_wo_footers.count(symbol):
            raise SystemExit(
                f"symbol {symbol!r} count changed: "
                f"source {source_body.count(symbol)} markdown {markdown_wo_footers.count(symbol)}"
            )
    source_urls = URL_RE.findall(source_body)
    md_urls = URL_RE.findall(markdown_wo_footers)
    if source_urls != md_urls:
        raise SystemExit(
            "URL mismatch\n"
            f"only source {set(source_urls) - set(md_urls)}\n"
            f"only md {set(md_urls) - set(source_urls)}"
        )
    if len(source_urls) != 31:
        raise SystemExit(f"expected 31 source URLs, found {len(source_urls)}")


def split_goals(playbook: str, goals_dir: Path) -> list[dict]:
    matches = list(GOAL_RE.finditer(playbook))
    if len(matches) != 30:
        raise SystemExit(f"expected 30 goals, found {len(matches)}")
    goals_dir.mkdir(parents=True, exist_ok=True)
    records = []
    pieces = []
    for index, match in enumerate(matches):
        number = int(match.group(1))
        if number != index + 1:
            raise SystemExit(f"goal number out of order: {number}")
        if index + 1 < len(matches):
            end = matches[index + 1].start()
        else:
            end = playbook.find("\f", match.start())
            if end < 0:
                raise SystemExit("goal 30 has no terminating form feed")
        chunk = playbook[match.start() : end]
        pieces.append(chunk)
        lines = chunk.splitlines()
        title = ""
        for line in lines[1:]:
            if line.strip():
                title = line.strip()
                break
        if not title:
            raise SystemExit(f"goal {number:02d} has no title")
        slug = slugify(title)
        path = goals_dir / f"{number:02d}-{slug}.md"
        path.write_text(chunk)
        required = [
            "Prerequisites:",
            "COPY PROMPT",
            "IMPLEMENTATION REQUIREMENTS",
            "ACCEPTANCE CHECKS",
            "REQUIRED HANDOFF",
        ]
        missing = [item for item in required if item not in chunk]
        if missing:
            raise SystemExit(f"goal {number:02d} missing {missing}")
        if len(GOAL_RE.findall(chunk)) != 1:
            raise SystemExit(f"goal {number:02d} does not contain exactly one goal header")
        if number < 30 and f"GOAL {number + 1:02d} / 30" in chunk:
            raise SystemExit(f"goal {number:02d} includes the next goal header")
        if not chunk.strip():
            raise SystemExit(f"goal {number:02d} is empty")
        records.append(
            {
                "number": number,
                "title": title,
                "slug": slug,
                "path": path,
                "bytes": len(chunk.encode()),
                "sha256": sha256(chunk.encode()),
            }
        )
    joined = "".join(pieces)
    expected = playbook[matches[0].start() : playbook.find("\f", matches[-1].start())]
    if joined != expected:
        raise SystemExit("joined goal slices do not equal the playbook span")
    goal30 = re.sub(r"\s+", " ", pieces[-1])
    goal01 = re.sub(r"\s+", " ", pieces[0])
    if "completing 30 prompts is not itself approval." not in goal30:
        raise SystemExit("goal 30 looks truncated")
    if "No application code is required for this goal." not in goal01:
        raise SystemExit("goal 01 looks truncated")
    return records


def build_agents(playbook: str) -> str:
    start = playbook.find("Copy this contract into the root AGENTS.md")
    end_marker = "never end-to-end verification."
    end = playbook.find(end_marker, start)
    if start < 0 or end < 0:
        raise SystemExit("could not find the AGENTS.md contract in the playbook")
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
    if not blocks or blocks[0][0] != "p":
        raise SystemExit("AGENTS.md contract did not start with the intro paragraph")
    meta = "Copy this contract into the root AGENTS.md before running the goals. "
    if not blocks[0][1].startswith(meta):
        raise SystemExit("expected the playbook meta sentence at the start of the contract")
    blocks[0] = ("p", blocks[0][1][len(meta) :].strip())
    rendered_lines = []
    for kind, text in blocks:
        if kind == "h":
            rendered_lines.append(f"## {text}")
            rendered_lines.append("")
        else:
            rendered_lines.append(text)
            rendered_lines.append("")
    contract = "\n".join(rendered_lines).rstrip() + "\n"
    expected_region = region.replace(meta, "", 1)
    if Counter(tokens(expected_region)) != Counter(tokens(contract)):
        missing = Counter(tokens(expected_region)) - Counter(tokens(contract))
        extra = Counter(tokens(contract)) - Counter(tokens(expected_region))
        raise SystemExit(f"AGENTS.md contract mismatch missing={missing} extra={extra}")
    contract = "# AGENTS.md\n\n" + contract
    substitution = """\

## Model substitution

The playbook names Codex models (Astra and GPT-6.1 Sol) and effort levels. This repository is built by Cursor cloud agents instead. Extra High goals (01-02 and 29-30) use the highest reasoning setting available.

Every docs/evidence/goal-NN.md must record the model actually used, the effort or reasoning setting actually used, and the playbook's assigned model and effort for that goal. Never claim a model or an effort setting was used that was not. Extra effort does not certify correctness.
"""
    return contract + substitution


def build_progress(records: list[dict]) -> str:
    rows = [
        "| Goal | Deliverable | Status | Evidence |",
        "| --- | --- | --- | --- |",
    ]
    for record in records:
        rel = f"goals/{record['path'].name}"
        rows.append(
            f"| {record['number']:02d} | [{record['title']}]({rel}) | NOT STARTED | |"
        )
    body = f"""\
# Progress

Tracker for goals 01-30. Goal 00 is this companion pack. It is not one of the thirty implementation goals.

## Status values

`IMPLEMENTED`, `VERIFIED`, `BLOCKED` and `DEFERRED` are different statuses.

- **IMPLEMENTED** — the goal's changes are in the branch, and its required checks have not all been run against the reported revision.
- **VERIFIED** — the goal's required checks actually ran against the reported revision. The evidence file records that revision, the commands, the exit codes and the results.
- **BLOCKED** — a required check, dependency, environment or decision is unavailable. A missing test environment is BLOCKED, never a pass.
- **DEFERRED** — the work is intentionally postponed, and the reason is recorded.

`NOT STARTED` means the goal has not been taken up. It is not one of the four evidence statuses.

A goal is verified only when its required checks actually ran against the reported revision. Never convert a missing test environment into a pass.

`docs/progress.tsv` is the machine-readable tracker. It currently contains the header row only.

## Goals

{chr(10).join(rows)}
"""
    return body


def build_readme() -> str:
    existing = (ROOT / "README.md").read_text()
    if not existing.startswith("# shortvideo-network"):
        raise SystemExit("README.md does not start with the expected title; refusing to merge")
    addition = """\
# shortvideo-network

Permissioned community beta for a decentralized short-video network with a custom application blockchain. The repository currently holds the companion pack: prompts and documentation, not a working application.

- Agent contract: [AGENTS.md](AGENTS.md)
- Blueprint: [docs/blueprint.md](docs/blueprint.md) and [docs/blueprint.pdf](docs/blueprint.pdf)
- Prompt playbook: [docs/prompt-playbook.txt](docs/prompt-playbook.txt) and [docs/prompt-playbook.pdf](docs/prompt-playbook.pdf)
- Goals 01-30: [docs/goals/](docs/goals/)
- Progress: [docs/progress.md](docs/progress.md) and [docs/progress.tsv](docs/progress.tsv)
- Evidence: [docs/evidence/README.md](docs/evidence/README.md)
"""
    if existing.strip() == "# shortvideo-network":
        return addition
    if "docs/goals/" in existing:
        return existing
    return existing.rstrip() + "\n\n" + addition


def build_evidence_readme() -> str:
    return """\
# Evidence

Each goal writes `docs/evidence/goal-NN.md` for the revision that was tested.

Record all of the following:

- tested commit
- runtime (operating system and the toolchain versions that were actually observed)
- commands actually run
- exit codes
- test counts (passed, failed and skipped)
- logs, or the locations of raw artifacts
- blockers
- model used

Do not invent logs, exit codes or test counts. A command that was not run is a blocker, not a pass. `IMPLEMENTED`, `VERIFIED`, `BLOCKED` and `DEFERRED` are distinct. A missing test environment is `BLOCKED`, never a pass.

Record the model that actually ran the goal. The playbook names Codex models (Astra and GPT-6.1 Sol) as a suggested allocation. Do not claim one of those models was used unless it was.

Do not put secrets, private keys, personal data or live credentials in an evidence file.

This companion pack is recorded in [goal-00.md](goal-00.md).
"""


def build_gitignore() -> str:
    return """\
# Secrets
.env
.env.*
!.env.example
secrets/

# Key files
*.pem
*.key
*.p12
*.pfx
*.keystore
id_rsa
id_rsa.*
id_ed25519
id_ed25519.*

# Chain key files
priv_validator_key.json
node_key.json
priv_validator_state.json
keyring-*/

# Mnemonic and seed files
*mnemonic*
*.seed
seed.txt
seed.json
seeds.txt
seeds.json
*seed-phrase*
*seed_phrase*

# Media uploads
/uploads/
**/media-uploads/
**/user-uploads/

# Database dumps
*.dump
*.sql
*.sql.gz
*.sqlite
*.sqlite3
*.db

# Build output
node_modules/
dist/
build/
bin/
coverage/
*.log
.cache/
__pycache__/
"""


def generated_tree(playbook: str, blueprint_text: str, goals_dir: Path) -> dict[str, str]:
    records = split_goals(playbook, goals_dir)
    markdown, _subsections, body = build_blueprint(blueprint_text)
    check_blueprint(body, markdown, blueprint_text)
    files = {
        "docs/blueprint.md": markdown,
        "AGENTS.md": build_agents(playbook),
        ".gitignore": build_gitignore(),
    }
    for record in records:
        rel = f"docs/goals/{record['path'].name}"
        files[rel] = record["path"].read_text()
    print(f"generated_goal_files {len(records)}")
    for record in records:
        print(
            f"{record['number']:02d} {record['path'].name} bytes={record['bytes']} sha256={record['sha256']}"
        )
    return files


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--write",
        action="store_true",
        help="rewrite goals, docs/blueprint.md, AGENTS.md, and .gitignore",
    )
    args = parser.parse_args()
    if not PLAYBOOK_TXT.is_file() or not BLUEPRINT_TXT.is_file():
        raise SystemExit(f"missing source {PLAYBOOK_TXT} or {BLUEPRINT_TXT}")
    playbook = PLAYBOOK_TXT.read_text()
    blueprint_text = BLUEPRINT_TXT.read_text()
    with tempfile.TemporaryDirectory() as tmp:
        files = generated_tree(playbook, blueprint_text, Path(tmp) / "goals")
    mismatches = []
    for rel, text in sorted(files.items()):
        path = ROOT / rel
        if args.write:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text)
            print(f"wrote {rel}")
            continue
        current = path.read_text() if path.is_file() else None
        if current != text:
            mismatches.append(rel)
            print(f"MISMATCH {rel}")
        else:
            print(f"MATCH {rel}")
    if not args.write and mismatches:
        raise SystemExit(f"check_failed {len(mismatches)} files differ")
    print("generator_check_exit 0")


if __name__ == "__main__":
    try:
        main()
    except BrokenPipeError:
        sys.exit(0)
