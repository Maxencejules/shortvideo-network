#!/usr/bin/env python3
"""Check the Goal 01 specification for traceability.

Run from the repository root:

    python3 docs/evidence/goal-01/check_requirements.py

Exit 0 means the assertions in this file held for this worktree.
A pass is a document check. It is not a chain test, a security
audit, or an approval of the protocol.
"""

from __future__ import annotations

import hashlib
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
REQ = ROOT / "docs" / "requirements.md"
THREAT = ROOT / "docs" / "threat-model.md"
DECISIONS = ROOT / "docs" / "decision-register.md"
PROGRESS = ROOT / "docs" / "progress.md"
PROGRESS_TSV = ROOT / "docs" / "progress.tsv"
EVIDENCE = ROOT / "docs" / "evidence" / "goal-01.md"

ROLES = {
    "product/application",
    "chain/security",
    "media/provider agents",
    "deployment/on-call",
    "trust/safety",
}
SCOPES = {"IN_SCOPE", "DEFERRED", "NON_GOAL"}
GOAL_RE = re.compile(r"^(0[1-9]|[12][0-9]|30)$")
ID_RE = re.compile(r"^REQ-[A-Z]+-\d{3}$")
DEC_RE = re.compile(r"^DEC-\d{3}$")
PAGE_PART_RE = re.compile(r"^(\d{1,2})(?:-(\d{1,2}))?$")
FENCE = "requirements-matrix"
HEADER = "id\tscope\tpages\trole\tgoals\tdisposition\tsummary"

REQUIRED_DECISIONS = [f"DEC-{i:03d}" for i in range(1, 9)]
REQUIRED_THREATS = [
    "TM-UPLOAD",
    "TM-UNAUTH-TX",
    "TM-KEYS",
    "TM-DISHONEST",
    "TM-REPLICAS",
    "TM-STALL",
    "TM-STALE-GW",
    "TM-ABUSE",
    "TM-SUPPLY",
    "TM-VALIDATOR",
    "TM-STAKE-BYPASS",
    "TM-PERSONAL",
    "TM-INJECTION",
    "TM-MINT",
    "TM-SECRET",
]
SELF_CHECK_IDS = {
    "REQ-PROD-012",
    "REQ-SCOPE-002",
    "REQ-SCOPE-006",
    "REQ-SCOPE-008",
    "REQ-SCOPE-010",
    "REQ-SCOPE-020",
    "REQ-CHAIN-014",
    "REQ-MOD-009",
    "REQ-GOV-007",
    "REQ-CAP-006",
    "REQ-ROAD-001",
    "REQ-ROAD-002",
    "REQ-ROAD-003",
    "REQ-ROAD-004",
    "REQ-REPO-004",
    "REQ-RISK-001",
    "REQ-RISK-008",
}
STATUSES = {"NOT STARTED", "IMPLEMENTED", "VERIFIED", "BLOCKED", "DEFERRED"}
REQUIRED_SCENARIOS = [
    "S-PLAYBACK",
    "S-ORIGIN",
    "S-DEPART",
    "S-STALL",
    "S-REMOVE",
]
REQUIRED_CONTRADICTIONS = [f"CR-{i:03d}" for i in range(1, 14)]
PROPOSED_IDS = {
    "REQ-SCOPE-012",
    "REQ-SCOPE-014",
    "REQ-SCOPE-015",
    "REQ-TECH-001",
    "REQ-TECH-002",
    "REQ-TECH-003",
    "REQ-TECH-004",
    "REQ-TECH-005",
    "REQ-TECH-006",
    "REQ-TECH-008",
    "REQ-TECH-009",
    "REQ-TECH-013",
    "REQ-CHAIN-003",
    "REQ-HOST-002",
    "REQ-HOST-005",
    "REQ-GEN-003",
    "REQ-GOV-003",
    "REQ-GOV-005",
    "REQ-ID-004",
    "REQ-QA-005",
    "REQ-QA-006",
    "REQ-QA-007",
    "REQ-QA-010",
    "REQ-CAP-003",
    "REQ-CAP-006",
}
GOAL_FILE_SHA256 = {
    "01-requirements-scope-and-threat-model.md": "bcc0736a330d9d41bbd396ba0b41ab232ee28ef5b0ec0774a3643e5d595ec1f1",
    "02-protocol-schemas-and-invariants.md": "d71f0d283415b0984fbc6744de2353af7b8185c9d08bc0f42a3f07b1d2019ffd",
    "03-monorepo-toolchains-and-ci.md": "c395c35957c111d8371a8500025b33e5a6d5b4e478ea42036d8ecae2017e6e12",
    "04-codex-cloud-development-environment.md": "90b4fc90b4978980371194f2c5404f5284efff6eb847fba0fb6361dd62acb248",
    "05-chain-application-genesis-and-devnet.md": "86d78b6bb3b6286e3260ec6e95329e76d1f381421007bcbccd88c839d0f315c0",
    "06-authority-and-validator-lifecycle.md": "fa746f4f8397e8795786e126a8d5625415921f2474ae9a758875f405c144f81e",
    "07-provider-and-content-registries.md": "dc45bfdd58cc3473a4e8bbf6932f8e73c75c749ad8ce93ad28dcfa9c38afd591",
    "08-lease-lifecycle-and-capacity-accounting.md": "ffe7ab54dcb566fbc8bf3192b079b7ddb8595776f507d8b2200803e2d4edadce",
    "09-beta-attestations-and-audit-evidence.md": "8a4c239df18cbbed28db3d10f3180e4b052d2daed5096cdf32b5f661c53b5b2b",
    "10-policy-governance-and-chain-upgrades.md": "7f2fe6cc47460050211f795c48c800acb85849709d4056179c4d95a73c229471",
    "11-transaction-broadcaster-and-outbox.md": "dc3778d860ffe4944fb7654ecb39fd7fb501944c6958721845aea449a6c8a059",
    "12-committed-state-indexer-and-reconciliation.md": "8c35f458a44817757e1cd3c1685da786be4d86b8d217c9b8785346c77a943082",
    "13-authenticated-uploads-and-quarantine.md": "61a774103130673d1f8551c5e6fcc35d3635286f59af744691ade7a09d0843c8",
    "14-isolated-video-processing-workers.md": "7499ca9be27d94cba1a51880864b2acd6dc9913e082f0e26741a864de1d3ab73",
    "15-media-manifests-and-https-playback.md": "f75d25ae6527e21d45cec401a70db05801e5b58d165a4c28650657b762765f30",
    "16-storage-provider-agent.md": "eeef4e9c3bce7fa05440ffaf96140f15090e097a75340e6926e9707585b0bfef",
    "17-replication-monitoring-and-repair.md": "bcd663c3997f67a635dbb7a2dd2efc0abc9ddd99f4dca14cb2832d61e6b23167",
    "18-end-to-end-network-milestone.md": "3ae2c72fa688dd163d76ebea85a7a7ad736db82507a69547f673acbc4a7f3d9c",
    "19-accounts-recovery-and-data-portability.md": "838faee9fb1529f1f3f8cccc7394e7aa106fb669fb330ae00f3533c4221af511",
    "20-viewer-web-application.md": "e2a30e6cb87179ddd7a83f25fc6e7dc7713d35b9da2380643eb5168ddc7262a7",
    "21-creator-publishing-interface.md": "31cb30290c9dc0ce2cde13d57ae75cc4c70591add069e43c80bc5230eba1a42c",
    "22-social-features-search-and-feeds.md": "4ba70a3eb17d102ad3b6be7602a528fd9ad04b3426661343e769164551dd0734",
    "23-moderation-blocking-and-removal.md": "d4d5a105d9f80c6d2c608c18f20600fc9f7c1cbf5735ab88d432767f9464939e",
    "24-operator-dashboard-and-node-onboarding.md": "98689fd08e9ae603ccac23aba866f6c03da2d8f81562be62fdffa90ddc720731",
    "25-observability-quotas-and-usage-controls.md": "1cd1aea7c999160f8fbee42ccb2bb9788bb2381fd53ec56c8890c1c170aceb20",
    "26-backups-restoration-and-signer-safety.md": "6d8ebf1ba4d3dfdd6e3a797b9f05ca335fb4244406f2a4f05336d5b4a69cf926",
    "27-packaging-installation-and-release-preparation.md": "547d159092d4f1e59cb0b4516092e5b68f20b948a557eae07f0d6dec17728db1",
    "28-performance-load-and-capacity-evidence.md": "ad8c99603f1ef4facd6ffd5fed567098d7d3e20c25e2ecf8a1a7f227d7033284",
    "29-adversarial-review-and-fault-testing.md": "011cf9fbadb66e5f11cd20b17940bd0bb76cf0bdbcebdf83ff87d50878a06db9",
    "30-final-acceptance-and-independent-installation.md": "e30ea2e467cc478f4c2e1773dcd1c6b36f550674c269df3c55489ab260255d6d",
}
ABSENT_ROOTS = [
    "apps",
    "services",
    "chain",
    "proto",
    "agents",
    "workers",
    "packages",
    "deployments",
    "tests",
]
SOURCE_SUFFIXES = {".go", ".ts", ".tsx", ".jsx", ".rs", ".sol", ".proto"}
PY_ALLOW = (
    "docs/evidence/goal-00/",
    "docs/evidence/goal-01/",
    "docs/evidence/goal-02/",
)
# Goal 02 protocol drafts and fixture runners are specification artifacts.
# They are not the chain, the client, or a service implementation.
SOURCE_ALLOW = (
    "docs/protocol/",
    "docs/evidence/goal-02/",
)


def exact_section(text: str, heading: str) -> str | None:
    """Return the body under a heading line that equals `heading`.

    A longer heading such as `## TM-SECRETS` does not match `## TM-SECRET`.
    """
    lines = text.splitlines()
    for index, line in enumerate(lines):
        if line == heading:
            body: list[str] = []
            for later in lines[index + 1 :]:
                if later.startswith("## "):
                    break
                body.append(later)
            return "\n".join(body)
    return None


def goals_named_in_text(text: str) -> set[int]:
    """Goal numbers named in a TEST disposition, including `Goals 1, 2, and 3`."""
    found: set[int] = set()
    for match in re.finditer(r"\b[Gg]oal\s+(\d{1,2})\b", text):
        found.add(int(match.group(1)))
    for match in re.finditer(
        r"\b[Gg]oals\s+((?:\d{1,2}\b(?:\s*,\s*and\s+|\s*,\s*|\s+and\s+)?)*)",
        text,
    ):
        for number in re.findall(r"\d{1,2}", match.group(1)):
            found.add(int(number))
    return found


def column_goals(raw: str) -> set[int]:
    if raw == "none":
        return set()
    return {int(part) for part in raw.split(",") if part}


def check_row_goals(rows: list[dict[str, str]]) -> list[str]:
    """The goals column and the goals named in a TEST disposition are the same set."""
    errors: list[str] = []
    for row in rows:
        if row["scope"] != "IN_SCOPE" or not row["disposition"].startswith("TEST:"):
            continue
        named = goals_named_in_text(row["disposition"])
        column = column_goals(row["goals"])
        if named != column:
            errors.append(
                f"{row['id']} goals {sorted(column)} != test goals {sorted(named)}"
            )
    return errors


def parse_progress_table(progress: str) -> dict[str, str]:
    found: dict[str, str] = {}
    for goal in [f"{i:02d}" for i in range(0, 31)]:
        token = f"| {goal} |"
        if token not in progress:
            continue
        row = progress.split(token, 1)[1].split("\n", 1)[0]
        cells = [cell.strip() for cell in row.split("|")]
        found[goal] = cells[1] if len(cells) > 1 else ""
    return found


def check_progress_agreement(progress: str, tsv: str) -> list[str]:
    """Started goals in the markdown table and progress.tsv have the same status."""
    errors: list[str] = []
    lines = tsv.splitlines()
    if not lines or not lines[0].startswith("goal\tstatus\t"):
        return ["progress.tsv header missing"]
    statuses: dict[str, str] = {}
    for line in lines[1:]:
        if not line.strip():
            continue
        parts = line.split("\t")
        if len(parts) < 2:
            errors.append("progress.tsv short row")
            continue
        goal, status = parts[0], parts[1]
        if goal in statuses:
            errors.append(f"progress.tsv duplicate goal {goal}")
        statuses[goal] = status
        if status not in STATUSES:
            errors.append(f"progress.tsv goal {goal} status {status!r} is not a known status")
    table = parse_progress_table(progress)
    for goal, status in statuses.items():
        if goal not in table:
            errors.append(f"progress.tsv goal {goal} missing from progress.md")
        elif table[goal] != status:
            errors.append(
                f"progress status mismatch {goal}: markdown {table[goal]} tsv {status}"
            )
    for goal, status in table.items():
        if status == "NOT STARTED":
            if goal in statuses:
                errors.append(f"progress.tsv includes NOT STARTED goal {goal}")
        elif goal not in statuses:
            errors.append(f"progress.tsv missing started goal {goal}")
    return errors


def expand_pages(raw: str) -> set[int]:
    pages: set[int] = set()
    for part in raw.split(","):
        match = PAGE_PART_RE.fullmatch(part)
        if not match:
            raise ValueError(f"bad page token {part!r}")
        start = int(match.group(1))
        end = int(match.group(2) or match.group(1))
        if start < 1 or end > 40 or end < start:
            raise ValueError(f"bad page range {part!r}")
        pages.update(range(start, end + 1))
    return pages


def parse_matrix(text: str) -> tuple[list[dict[str, str]], list[str]]:
    errors: list[str] = []
    marker = f"```{FENCE}"
    if text.count(marker) != 1:
        return [], [f"expected exactly one {marker} fence, found {text.count(marker)}"]
    try:
        body = text.split(marker, 1)[1].split("```", 1)[0]
    except IndexError:
        return [], ["matrix fence did not close"]
    lines = [line for line in body.splitlines() if line.strip()]
    if not lines:
        return [], ["matrix fence is empty"]
    if lines[0] != HEADER:
        return [], [f"matrix header {lines[0]!r} != {HEADER!r}"]
    rows: list[dict[str, str]] = []
    seen: set[str] = set()
    for line_no, line in enumerate(lines[1:], start=2):
        fields = line.split("\t")
        if len(fields) != 7:
            errors.append(f"matrix line {line_no} has {len(fields)} fields")
            continue
        row = dict(zip(["id", "scope", "pages", "role", "goals", "disposition", "summary"], fields, strict=True))
        if not ID_RE.fullmatch(row["id"]):
            errors.append(f"bad id {row['id']}")
        if row["id"] in seen:
            errors.append(f"duplicate id {row['id']}")
        seen.add(row["id"])
        if row["scope"] not in SCOPES:
            errors.append(f"{row['id']} bad scope {row['scope']}")
        if row["role"] not in ROLES:
            errors.append(f"{row['id']} bad role {row['role']}")
        if not row["summary"].strip():
            errors.append(f"{row['id']} empty summary")
        try:
            row["_pages"] = expand_pages(row["pages"])  # type: ignore[assignment]
        except ValueError as exc:
            errors.append(f"{row['id']} {exc}")
            row["_pages"] = set()  # type: ignore[assignment]
        rows.append(row)
    return rows, errors


def validate_rows(rows: list[dict[str, str]], decision_ids: set[str]) -> list[str]:
    errors: list[str] = []
    referenced_decisions: set[str] = set()
    for row in rows:
        disposition = row["disposition"]
        if "APPROVED" in disposition and "not approved" not in disposition.lower():
            errors.append(f"{row['id']} disposition looks approved")
        if row["scope"] == "IN_SCOPE":
            goals = row["goals"].split(",")
            if not goals or any(not GOAL_RE.fullmatch(goal) for goal in goals):
                errors.append(f"{row['id']} bad goals {row['goals']}")
            if disposition.startswith("TEST:"):
                if len(disposition) < 25:
                    errors.append(f"{row['id']} test text is too short")
            elif disposition.startswith("BLOCKER:"):
                match = re.match(r"BLOCKER:(DEC-\d{3})\b", disposition)
                if not match:
                    errors.append(f"{row['id']} blocker must name DEC-nnn")
                else:
                    dec = match.group(1)
                    referenced_decisions.add(dec)
                    if dec not in decision_ids:
                        errors.append(f"{row['id']} references missing {dec}")
            else:
                errors.append(f"{row['id']} needs TEST: or BLOCKER:")
        else:
            if row["goals"] != "none":
                errors.append(f"{row['id']} {row['scope']} must use goals none")
            if not disposition.startswith("OUT:"):
                errors.append(f"{row['id']} {row['scope']} must use OUT:")
            if len(disposition) < 15:
                errors.append(f"{row['id']} disposition too short")
        if row["id"] in PROPOSED_IDS and "PROPOSED" not in row["summary"]:
            errors.append(f"{row['id']} summary must say PROPOSED")
        if row["id"] in SELF_CHECK_IDS:
            if row["scope"] != "IN_SCOPE" or "SELF-CHECK" not in row["disposition"]:
                errors.append(f"{row['id']} must be an in-scope SELF-CHECK")
    for req_id in SELF_CHECK_IDS:
        if req_id not in {row["id"] for row in rows}:
            errors.append(f"missing self-check {req_id}")
    for dec in REQUIRED_DECISIONS:
        if dec not in referenced_decisions:
            errors.append(f"{dec} is not a blocker on any IN_SCOPE row")
    by_id = {row["id"]: row for row in rows}
    required_text = {
        "REQ-PROD-013": "no tradable token",
        "REQ-OFF-001": "Video bytes stay off-chain",
        "REQ-SCOPE-002": "Public launch: DISABLED",
    }
    for req_id, snippet in required_text.items():
        row = by_id.get(req_id)
        if row is None:
            errors.append(f"missing {req_id}")
        elif snippet not in row["summary"]:
            errors.append(f"{req_id} summary missing {snippet!r}")
        elif row["scope"] != "IN_SCOPE":
            errors.append(f"{req_id} must be IN_SCOPE")
    for bound in range(1, 7):
        req_id = f"REQ-BOUND-{bound:03d}"
        if req_id not in by_id or by_id[req_id]["scope"] != "IN_SCOPE":
            errors.append(f"missing trust boundary {req_id}")
    return errors


def check_blueprint_page_coverage(rows: list[dict[str, str]]) -> list[str]:
    covered: set[int] = set()
    for row in rows:
        if row.get("scope") != "IN_SCOPE":
            continue
        pages = row.get("_pages")
        if not isinstance(pages, set):
            try:
                pages = expand_pages(str(row.get("pages", "")))
            except ValueError as exc:
                return [f"page coverage {exc}"]
        covered |= pages
    missing = [page for page in range(3, 29) if page not in covered]
    if missing:
        return [f"IN_SCOPE rows miss blueprint pages {missing}"]
    return []


def check_scenario_lists(req_text: str, scenarios: list[str] | None = None) -> list[str]:
    """Every REQ id in a scenario's prose must appear in its Requirements list."""
    errors: list[str] = []
    names = REQUIRED_SCENARIOS if scenarios is None else scenarios
    for scenario in names:
        marker = f"## {scenario}"
        if marker not in req_text:
            errors.append(f"missing scenario {scenario}")
            continue
        body = req_text.split(marker, 1)[1].split("\n## ", 1)[0]
        if "### Requirements" not in body:
            errors.append(f"{scenario} missing ### Requirements")
            continue
        prose, rest = body.split("### Requirements", 1)
        list_part = rest.split("\n### ", 1)[0]
        prose_ids = set(re.findall(r"REQ-[A-Z]+-\d{3}", prose))
        list_ids = set(re.findall(r"REQ-[A-Z]+-\d{3}", list_part))
        missing = sorted(prose_ids - list_ids)
        if missing:
            errors.append(f"{scenario} requirements list omits {', '.join(missing)}")
    return errors


def validate_documents(rows: list[dict[str, str]]) -> list[str]:
    errors: list[str] = []
    req_text = REQ.read_text(encoding="utf-8")
    threat = THREAT.read_text(encoding="utf-8")
    decisions = DECISIONS.read_text(encoding="utf-8")
    progress = PROGRESS.read_text(encoding="utf-8")
    for label, text in (
        ("requirements", req_text),
        ("threat-model", threat),
        ("decision-register", decisions),
        ("progress", progress),
    ):
        if "Public launch: DISABLED" not in text:
            errors.append(f"{label} missing Public launch: DISABLED")
    if "not an approved protocol" not in req_text:
        errors.append("requirements must say the protocol is not approved")
    for phrase in ("trustless", "permissionless", "Video bytes stay off-chain", "no tradable token"):
        if phrase not in req_text:
            errors.append(f"requirements missing {phrase!r}")
    if "No tradable token, coin, sale, or mining." not in req_text:
        errors.append("requirements missing the token exclusion sentence")
    if "## PROPOSED defaults" not in req_text:
        errors.append("missing PROPOSED defaults section")
    if req_text.count("PROPOSED") < 15:
        errors.append("PROPOSED appears too rarely in requirements")
    if "## Repository inventory" not in req_text:
        errors.append("missing repository inventory")
    for word in ("working code", "placeholder", "missing"):
        if word not in req_text.lower():
            errors.append(f"inventory missing the word {word}")
    if "## Proposed Goal 02 input checklist" not in req_text:
        errors.append("missing Goal 02 checklist")
    if "explicitly blocked" not in req_text:
        errors.append("Goal 02 checklist must say decisions are explicitly blocked")
    for dec in REQUIRED_DECISIONS:
        if dec not in req_text:
            errors.append(f"requirements never mention {dec}")
        if f"## {dec}" not in decisions:
            errors.append(f"decision register missing {dec}")
        else:
            section = decisions.split(f"## {dec}", 1)[1].split("\n## ", 1)[0]
            if "### Question" not in section or "### PROPOSED default" not in section or "### Blocks" not in section:
                errors.append(f"{dec} missing required subsections")
            if "Status: OPEN" not in section:
                errors.append(f"{dec} is not OPEN")
            if re.search(r"^Status: APPROVED\s*$", section, re.M):
                errors.append(f"{dec} is marked APPROVED")
    topics = (
        "custody",
        "recovery",
        "operator",
        "moderation",
        "removal",
        "retention",
        "sponsorship",
        "licen",
        "audience",
        "quorum",
    )
    lowered = decisions.lower()
    for topic in topics:
        if topic not in lowered:
            errors.append(f"decision register missing topic {topic}")
    for scenario in REQUIRED_SCENARIOS:
        if f"## {scenario}" not in req_text:
            errors.append(f"missing scenario {scenario}")
            continue
        body = req_text.split(f"## {scenario}", 1)[1].split("\n## ", 1)[0]
        for heading in ("### Expected", "### Not promised", "### Requirements", "### Open decisions"):
            if heading not in body:
                errors.append(f"{scenario} missing {heading}")
        if "REQ-" not in body or "DEC-" not in body:
            errors.append(f"{scenario} must cite a requirement and a decision")
    errors.extend(check_scenario_lists(req_text))
    for contradiction in REQUIRED_CONTRADICTIONS:
        if f"## {contradiction}" not in req_text:
            errors.append(f"missing {contradiction}")
            continue
        body = req_text.split(f"## {contradiction}", 1)[1].split("\n## ", 1)[0]
        if "### Resolution" not in body:
            errors.append(f"{contradiction} missing Resolution")
    if "independent process" not in threat.lower() or "independent operator" not in threat.lower():
        errors.append("threat model must distinguish process and operator")
    if "## Actors" not in threat:
        errors.append("threat model missing Actors")
    threat_phrases = {
        "TM-STAKE-BYPASS": ("x/authority", "MsgCreateValidator", "REQ-CHAIN-006"),
        "TM-PERSONAL": ("REQ-OFF-002", "REQ-OFF-004"),
        "TM-INJECTION": ("SQL", "prompt", "REQ-SEC-009"),
        "TM-MINT": ("x/mint", "x/distribution", "x/slashing"),
        "TM-SECRET": ("committed to git",),
    }
    for threat_id in REQUIRED_THREATS:
        body = exact_section(threat, f"## {threat_id}")
        if body is None:
            errors.append(f"missing exact heading ## {threat_id}")
            continue
        for heading in ("### Mitigation", "### Test", "### Residual limitation"):
            if heading not in body:
                errors.append(f"{threat_id} missing {heading}")
        for phrase in threat_phrases.get(threat_id, ()):
            if phrase not in body:
                errors.append(f"{threat_id} missing {phrase!r}")
    known = {row["id"] for row in rows}
    for path in (REQ, THREAT):
        for req_id in set(re.findall(r"REQ-[A-Z]+-\d{3}", path.read_text(encoding="utf-8"))):
            if req_id not in known:
                errors.append(f"{path.name} cites unknown {req_id}")
    for dec in set(re.findall(r"DEC-\d{3}", req_text + threat)):
        if dec not in REQUIRED_DECISIONS:
            errors.append(f"unknown decision cited: {dec}")
    for goal in [f"{i:02d}" for i in range(0, 31)]:
        if f"| {goal} |" not in progress and f"| {goal} " not in progress:
            errors.append(f"progress table missing goal {goal}")
            continue
        row = progress.split(f"| {goal} |", 1)[1].split("\n", 1)[0]
        cells = [cell.strip() for cell in row.split("|")]
        # cells: deliverable, status, ... because the split consumed the goal cell
        status = cells[1] if len(cells) > 1 else ""
        if status not in STATUSES and status != "NOT STARTED":
            errors.append(f"goal {goal} status {status!r} is not a known status")
    for phrase in (
        "Cloud commands",
        "Fuller Linux test runner",
        "Independent machines",
        "no dates",
        "no token totals",
        "no market adoption",
        "no production security",
    ):
        if phrase not in progress:
            errors.append(f"progress missing {phrase!r}")
    tsv = PROGRESS_TSV.read_text(encoding="utf-8")
    lines = tsv.splitlines()
    if not any(line.startswith("00\t") for line in lines):
        errors.append("progress.tsv lost goal 00")
    goal_01 = [line for line in lines if line.startswith("01\t")]
    if len(goal_01) != 1 or "\tgoal-01\t" not in goal_01[0]:
        errors.append("progress.tsv missing goal 01 row on branch goal-01")
    elif goal_01[0].split("\t")[1] not in STATUSES:
        errors.append("progress.tsv goal 01 status is not a known status")
    errors.extend(check_progress_agreement(progress, tsv))
    errors.extend(check_row_goals(rows))
    return errors


def validate_tree() -> list[str]:
    errors: list[str] = []
    for name in ABSENT_ROOTS:
        if (ROOT / name).exists():
            errors.append(f"application path exists: {name}")
    for path in ROOT.rglob("*"):
        if not path.is_file():
            continue
        if ".git" in path.parts:
            continue
        rel = path.relative_to(ROOT).as_posix()
        if path.suffix in SOURCE_SUFFIXES and not rel.startswith(SOURCE_ALLOW):
            errors.append(f"application source present: {rel}")
        if path.suffix == ".py" and not rel.startswith(PY_ALLOW):
            errors.append(f"unexpected python: {rel}")
    goals = ROOT / "docs" / "goals"
    for name, digest in GOAL_FILE_SHA256.items():
        file_hash = hashlib.sha256((goals / name).read_bytes()).hexdigest()
        if file_hash != digest:
            errors.append(f"goal page changed: {name}")
    return errors


def _one_row(body: str) -> str:
    return f"```{FENCE}\n{HEADER}\n{body}\n```\n"


def self_test() -> tuple[list[str], int]:
    """Each case is a separate negative fixture and must fail for its own reason."""
    problems: list[str] = []
    cases = 0

    def expect(name: str, errors: list[str], needle: str) -> None:
        nonlocal cases
        cases += 1
        if any(needle in item for item in errors):
            print(f"self_test {name} fail_as_expected")
            return
        print(f"self_test {name} MISSED")
        problems.append(f"self-test {name} did not fail with {needle!r}; got {errors}")

    dup = (
        "REQ-BAD-001\tIN_SCOPE\t3\tchain/security\t01\tTEST: a sufficiently long self-test disposition\tok\n"
        "REQ-BAD-001\tIN_SCOPE\t3\tchain/security\t01\tTEST: a sufficiently long self-test disposition\tok\n"
    )
    _rows, dup_errors = parse_matrix(_one_row(dup))
    expect("duplicate id", dup_errors, "duplicate id")

    _rows, role_parse = parse_matrix(_one_row(
        "REQ-BAD-002\tIN_SCOPE\t3\t\t01\tTEST: a sufficiently long self-test disposition\tok"
    ))
    expect("missing role", role_parse + validate_rows(_rows, set()), "bad role")

    _rows, goal_parse = parse_matrix(_one_row(
        "REQ-BAD-003\tIN_SCOPE\t3\tchain/security\t99\tTEST: a sufficiently long self-test disposition\tok"
    ))
    expect("bad goal", goal_parse + validate_rows(_rows, set()), "bad goals")

    _rows, defer_parse = parse_matrix(_one_row(
        "REQ-BAD-004\tDEFERRED\t5\tproduct/application\t05\tOUT: deferred work given an implementation goal\tlater"
    ))
    expect(
        "deferred with a goal",
        defer_parse + validate_rows(_rows, set()),
        "must use goals none",
    )

    _rows, open_parse = parse_matrix(_one_row(
        "REQ-BAD-005\tIN_SCOPE\t3\tchain/security\t01\tNOTE: neither a test assignment nor a decision blocker\tok"
    ))
    expect(
        "missing test or blocker",
        open_parse + validate_rows(_rows, set()),
        "needs TEST: or BLOCKER:",
    )

    page_errors = check_blueprint_page_coverage([
        {"scope": "IN_SCOPE", "pages": "3", "_pages": {3}},
    ])
    expect("missing blueprint page", page_errors, "miss blueprint pages")

    scenario_errors = check_scenario_lists(
        "## S-PLAYBACK\nThe prose cites REQ-MEDIA-002.\n### Requirements\nREQ-MEDIA-001\n### Open decisions\nDEC-001\n",
        ["S-PLAYBACK"],
    )
    expect("scenario list", scenario_errors, "requirements list omits")

    if exact_section("## TM-SECRETS\n", "## TM-SECRET") is None:
        heading_errors = ["missing exact heading ## TM-SECRET"]
    else:
        heading_errors = ["matched substring"]
    expect("threat heading exact", heading_errors, "missing exact heading")

    progress_errors = check_progress_agreement(
        "| 01 | item | VERIFIED | evidence | depends |\n",
        "goal\tstatus\tbranch\n01\tBLOCKED\tgoal-01\n",
    )
    expect("progress mismatch", progress_errors, "status mismatch")

    goal_errors = check_row_goals([
        {
            "id": "REQ-BAD-006",
            "scope": "IN_SCOPE",
            "goals": "20,24",
            "disposition": "TEST: Goal 30 success demonstration is the assigned check for this row.",
        }
    ])
    expect("row goals", goal_errors, "!= test goals")
    return problems, cases


def main() -> int:
    self_errors, self_cases = self_test()
    rows, parse_errors = parse_matrix(REQ.read_text(encoding="utf-8"))
    decision_ids = set(REQUIRED_DECISIONS)
    row_errors = validate_rows(rows, decision_ids) if rows else ["no rows"]
    page_errors = check_blueprint_page_coverage(rows) if rows else ["no rows for page coverage"]
    doc_errors = validate_documents(rows) if rows else []
    tree_errors = validate_tree()
    errors = self_errors + parse_errors + row_errors + page_errors + doc_errors + tree_errors
    in_scope = [row for row in rows if row["scope"] == "IN_SCOPE"]
    blocked = [row for row in in_scope if row["disposition"].startswith("BLOCKER:")]
    tested = [row for row in in_scope if row["disposition"].startswith("TEST:")]
    print(f"requirements {len(rows)}")
    print(f"in_scope {len(in_scope)}")
    print(f"with_test {len(tested)}")
    print(f"with_blocker {len(blocked)}")
    print(f"deferred {sum(row['scope'] == 'DEFERRED' for row in rows)}")
    print(f"non_goal {sum(row['scope'] == 'NON_GOAL' for row in rows)}")
    print(f"decisions {len(REQUIRED_DECISIONS)}")
    print(f"threats {len(REQUIRED_THREATS)}")
    print(f"scenarios {len(REQUIRED_SCENARIOS)}")
    print(f"contradictions {len(REQUIRED_CONTRADICTIONS)}")
    print(f"self_test_cases {self_cases}")
    print(f"self_test_problems {len(self_errors)}")
    print(f"page_coverage {'ok' if not page_errors else 'fail'}")
    print(f"errors {len(errors)}")
    for error in errors:
        print(f"ERROR {error}")
    if errors:
        print("result FAIL")
        return 1
    for scenario in REQUIRED_SCENARIOS:
        print(f"scenario {scenario} recorded")
    for contradiction in REQUIRED_CONTRADICTIONS:
        print(f"contradiction {contradiction} recorded")
    for threat_id in REQUIRED_THREATS:
        print(f"threat {threat_id} recorded")
    print("result PASS")
    print("note document check only; protocol not approved; public launch disabled")
    return 0


if __name__ == "__main__":
    sys.exit(main())
