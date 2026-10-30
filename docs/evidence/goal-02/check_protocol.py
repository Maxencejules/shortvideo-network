#!/usr/bin/env python3
"""Check the Goal 02 protocol contracts and fixtures.

Run from the repository root:

    python3 docs/evidence/goal-02/check_protocol.py

Exit 0 means the assertions in this file held for this worktree.
A pass is a schema and fixture check. It is not a devnet, a security
audit, or an approval of DEC-001 through DEC-008.
"""

from __future__ import annotations

import hashlib
import json
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(Path(__file__).resolve().parent))

import cases  # noqa: E402
import jcs  # noqa: E402

PROTOCOL = ROOT / "docs" / "protocol.md"
BOUNDS_PATH = ROOT / "docs" / "protocol" / "bounds.json"
PROTO_ROOT = ROOT / "docs" / "protocol" / "proto"
VECTORS = ROOT / "docs" / "protocol" / "fixtures" / "vectors.json"
DECISIONS = ROOT / "docs" / "decision-register.md"
PROGRESS = ROOT / "docs" / "progress.md"
PROGRESS_TSV = ROOT / "docs" / "progress.tsv"

REQUIRED_HEADINGS = [
    "## Freeze",
    "## Hard exclusions",
    "## Trust boundaries",
    "## Identifiers, integers, time, and digests",
    "## Signature domains",
    "## Error codes",
    "## Open decision parameters",
    "## Canonicalization",
    "## Chain transaction envelope",
    "## State machines",
    "## Transitions",
    "## Expiry and determinism",
    "## Privacy, tombstones, and reconciliation",
    "## Off-chain records",
    "## API contracts",
    "## Genesis",
    "## Page 26 failure tests",
    "## Invariants and implementation",
    "## Handoff",
]
REQUIRED_PHRASES = [
    "Public launch: DISABLED",
    "not an approved protocol",
    "frozen-pending-Gate-A",
    "Hashing is not anonymization",
    "SIGN_MODE_DIRECT",
    "SVN-JCS-1",
    "RFC 8785",
    "x/mint",
    "x/distribution",
    "x/slashing",
    "MsgCreateValidator",
    "Committed chain time",
    "local clock",
    "ATTESTATION_NOT_STORAGE_PROOF",
    "LEASE_STATE_AT_RISK",
    "REACHABILITY_DEGRADED",
    "history_erased",
    "DEC-001",
    "DEC-008",
    "PROPOSED_TEST_PARAMETER",
]
REQUIRED_CASES = {
    "boundary-id-64",
    "boundary-id-65",
    "malformed-missing-field",
    "unknown-field",
    "duplicate-lease",
    "wrong-domain",
    "expired-offer",
    "unauthorized-signer",
    "forged-signature",
    "replay-same-business",
    "replay-different-payload",
    "stall-one-validator",
    "stall-two-validators",
    "corrupt-manifest",
    "replica-match",
    "withdrawn-blocks-offer",
    "withdrawn-blocks-revival",
    "underflow-double-end",
    "overflow-reserve",
    "stale-term-version",
    "conflict-governance",
    "evidence-expired-cannot-renew",
    "evidence-does-not-extend-lease",
    "decision-blocked-without-devnet",
    "devnet-does-not-approve-decision",
    "no-local-clock-field",
    "zero-fee",
    "nonzero-fee",
    "reject-msg-create-validator",
    "reject-mint",
    "publication-separate",
    "reachability-not-lease-state",
    "capacity-exceeded",
    "idempotent-retry-no-double-reserve",
    "bounded-expiry",
    "drain-stops-new-offers",
    "rotate-key-stale-generation",
    "storage-proof-rejected",
    "host-custody-not-assumed",
    "renew-succeeds",
    "renew-changed-bytes",
    "update-provider",
    "update-provider-below-reserved",
    "admit-blocked-by-dec-002",
    "admit-non-authority",
    "second-withdraw",
    "approve-same-seat-replay",
    "propose-stale-base",
    "propose-dec-named-key",
    "execute-before-delay",
    "authority-end-lease",
    "rotate-then-old-key-fails",
    "provider-disabled",
    "proposal-expired",
    "proposal-rejected",
    "validator-power-applied",
    "evidence-rejected",
    "force-withdraw",
    "force-withdraw-non-authority",
    "replay-app-hash",
    "hosting-resilience",
}
REQUIRED_VALID_IDS = {
    "empty-object",
    "sort-keys",
    "whitespace",
    "nested",
    "newline",
    "control",
    "unicode",
    "slash",
    "quote",
    "safe-max",
    "safe-min",
    "letter-escape",
    "zero",
}
REQUIRED_INVALID_IDS = {
    "null",
    "fraction",
    "exponent",
    "leading-zero",
    "negative-zero",
    "unsafe-integer",
    "duplicate-key",
    "trailing-comma",
    "bad-key",
    "unicode-key",
    "lone-surrogate",
    "above-int64",
    "bom",
    "invalid-utf8",
}
CLIENT_PATHS = [
    "/upload-intents",
    "/uploads/{upload_id}",
    "/publish",
    "/feeds",
    "/reports",
    "/removal",
    "/exports",
]
PROVIDER_PATHS = [
    "/capability",
    "/offers/{offer_id}/acceptance",
    "/packages/{content_id}/{content_version}",
    "/transfers/{transfer_id}/receipt",
    "/inventory",
]
REQUIRED_EVENTS = [
    "ContentRegistered",
    "LeaseAccepted",
    "LeaseRenewed",
    "LeaseEnded",
    "EvidenceAccepted",
    "PolicyChanged",
]
FIELD_RE = re.compile(r"^\s*(?:repeated\s+)?(?:map<\s*)?[\w.]+\s+(\w+)\s*=\s*\d+\s*;", re.M)


def sha256_file(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def validate_schema(instance: object, schema: dict, path: str = "$") -> list[str]:
    errors: list[str] = []
    if "const" in schema and instance != schema["const"]:
        errors.append(f"{path} const")
    if "enum" in schema and instance not in schema["enum"]:
        errors.append(f"{path} enum")
    kind = schema.get("type")
    if kind == "object":
        if not isinstance(instance, dict):
            return errors + [f"{path} type"]
        properties = schema.get("properties", {})
        if schema.get("additionalProperties") is False:
            for key in instance:
                if key not in properties:
                    errors.append(f"{path}.{key} unknown")
        for key in schema.get("required", []):
            if key not in instance:
                errors.append(f"{path}.{key} missing")
        for key, sub in properties.items():
            if key in instance:
                errors.extend(validate_schema(instance[key], sub, f"{path}.{key}"))
    elif kind == "string":
        if not isinstance(instance, str):
            return errors + [f"{path} type"]
        if "minLength" in schema and len(instance) < schema["minLength"]:
            errors.append(f"{path} minLength")
        if "maxLength" in schema and len(instance) > schema["maxLength"]:
            errors.append(f"{path} maxLength")
        if "pattern" in schema and re.fullmatch(schema["pattern"], instance) is None:
            errors.append(f"{path} pattern")
    elif kind == "integer":
        if isinstance(instance, bool) or not isinstance(instance, int):
            return errors + [f"{path} type"]
        if "minimum" in schema and instance < schema["minimum"]:
            errors.append(f"{path} minimum")
        if "maximum" in schema and instance > schema["maximum"]:
            errors.append(f"{path} maximum")
    elif kind == "boolean":
        if not isinstance(instance, bool):
            errors.append(f"{path} type")
    elif kind == "array":
        if not isinstance(instance, list):
            return errors + [f"{path} type"]
        if "minItems" in schema and len(instance) < schema["minItems"]:
            errors.append(f"{path} minItems")
        if "maxItems" in schema and len(instance) > schema["maxItems"]:
            errors.append(f"{path} maxItems")
        item_schema = schema.get("items")
        if isinstance(item_schema, dict):
            for index, item in enumerate(instance):
                errors.extend(validate_schema(item, item_schema, f"{path}[{index}]"))
    return errors


def check_text(protocol: str, decisions: str) -> list[str]:
    errors: list[str] = []
    for heading in REQUIRED_HEADINGS:
        if heading not in protocol:
            errors.append(f"protocol missing {heading}")
    for phrase in REQUIRED_PHRASES:
        if phrase not in protocol:
            errors.append(f"protocol missing {phrase!r}")
    if "Status: APPROVED" in protocol:
        errors.append("protocol marks a status approved")
    for decision_id in [f"DEC-{i:03d}" for i in range(1, 9)]:
        if decision_id not in protocol:
            errors.append(f"protocol missing {decision_id}")
        if f"## {decision_id}" not in decisions:
            errors.append(f"register missing {decision_id}")
            continue
        section = decisions.split(f"## {decision_id}", 1)[1].split("\n## ", 1)[0]
        if "Status: OPEN" not in section:
            errors.append(f"{decision_id} is not OPEN")
        if re.search(r"^Status: APPROVED\s*$", section, re.M):
            errors.append(f"{decision_id} is APPROVED")
    return errors


def check_protos(bounds: dict) -> list[str]:
    errors: list[str] = []
    protos = sorted(PROTO_ROOT.rglob("*.proto"))
    if len(protos) < 20:
        errors.append("too few proto files")
    text = "\n".join(path.read_text(encoding="utf-8") for path in protos)
    if "map<" in text:
        errors.append("proto uses a map")
    if "message MsgCreateValidator" in text:
        errors.append("MsgCreateValidator is defined")
    for name in REQUIRED_EVENTS + ["AppGenesis", "BlockedDecisionParameter", "Tombstone", "MsgAcceptLease"]:
        if f"message {name} " not in text and f"message {name}{{" not in text:
            errors.append(f"proto missing {name}")
    for name in bounds["forbidden_chain_fields"]:
        for path in protos:
            if "offchain" in path.as_posix():
                continue
            for match in FIELD_RE.finditer(path.read_text(encoding="utf-8")):
                if match.group(1) == name:
                    errors.append(f"{path.name} field {name}")
    for domain in bounds["domains"].values():
        if domain not in (ROOT / "docs" / "protocol.md").read_text(encoding="utf-8"):
            errors.append(f"protocol missing domain {domain}")
    try:
        completed = subprocess.run(
            ["protoc", f"--proto_path={PROTO_ROOT}", "--descriptor_set_out=/tmp/svn-protocol.pb", *[str(path) for path in protos]],
            cwd=ROOT,
            capture_output=True,
            text=True,
            check=False,
        )
    except FileNotFoundError:
        print("protoc BLOCKED")
        errors.append("protoc BLOCKED")
        return errors
    print(f"protoc exit:{completed.returncode}")
    if completed.returncode != 0:
        errors.append(f"protoc failed {completed.stderr.strip()}")
    elif completed.stderr.strip():
        errors.append(f"protoc warning {completed.stderr.strip()}")
    else:
        errors.extend(scan_real_descriptor(Path("/tmp/svn-protocol.pb")))
        errors.extend(scan_negative_descriptors())
    return errors


def decode_descriptor(pb_path: Path) -> str:
    completed = subprocess.run(
        [
            "protoc",
            "--decode=google.protobuf.FileDescriptorSet",
            "--proto_path=/usr/include",
            "google/protobuf/descriptor.proto",
        ],
        input=pb_path.read_bytes(),
        capture_output=True,
        check=False,
    )
    if completed.returncode != 0:
        raise RuntimeError(completed.stderr.decode())
    return completed.stdout.decode()


def descriptor_hits(text: str) -> set[str]:
    hits: set[str] = set()
    if re.search(r"type: TYPE_(DOUBLE|FLOAT)\b", text):
        hits.add("float")
    if "google.protobuf.Timestamp" in text:
        hits.add("timestamp")
    for name, label in (
        ("MsgMint", "mint"),
        ("MsgSlashValidator", "slash"),
        ("MsgCreateValidator", "create-validator"),
        ("video_data", "video_data"),
        ("uploader_ip", "uploader_ip"),
        ("reward_amount", "reward_amount"),
        ("video_bytes", "video_bytes"),
    ):
        if re.search(rf'name: "{name}"', text):
            hits.add(label)
    if re.search(r'(?:^|[^\w])name: "[^"]*STORAGE_PROOF[^"]*"', text):
        hits.add("storage-proof")
    return hits


def scan_real_descriptor(pb_path: Path) -> list[str]:
    text = decode_descriptor(pb_path)
    hits = descriptor_hits(text)
    errors = [f"descriptor hit {hit}" for hit in sorted(hits)]
    for name in ("MsgAdmitProvider", "MsgForceWithdraw", "MsgDisableProvider", "ProposalPayload", "ValidatorPowerChange"):
        if f'name: "{name}"' not in text:
            errors.append(f"descriptor missing {name}")
    return errors


def scan_negative_descriptors() -> list[str]:
    errors: list[str] = []
    expected = {
        "timestamp.proto": "timestamp",
        "mint.proto": "mint",
        "slash.proto": "slash",
        "media.proto": "video_data",
        "storage_proof.proto": "storage-proof",
    }
    negative = Path(__file__).resolve().parent / "negative"
    for name, needle in expected.items():
        proto = negative / name
        out = Path("/tmp") / f"negative-{name}.pb"
        completed = subprocess.run(
            [
                "protoc",
                f"--proto_path={proto.parent}",
                "--proto_path=/usr/include",
                f"--descriptor_set_out={out}",
                str(proto),
            ],
            capture_output=True,
            text=True,
            check=False,
        )
        if completed.returncode != 0:
            errors.append(f"negative proto {name} did not compile")
            continue
        hits = descriptor_hits(decode_descriptor(out))
        if needle not in hits:
            errors.append(f"negative {name} missed {needle}")
        if name == "media.proto" and not {"float", "uploader_ip", "reward_amount"} <= hits:
            errors.append(f"negative media hits {sorted(hits)}")
    return errors


def closed_schema_errors(schema: dict, path: str = "$") -> list[str]:
    errors: list[str] = []
    if schema.get("type") == "object":
        if schema.get("additionalProperties") is not False:
            errors.append(f"{path} additionalProperties")
        for key, sub in schema.get("properties", {}).items():
            if isinstance(sub, dict):
                if sub.get("type") == "string" and "maxLength" not in sub and "const" not in sub:
                    errors.append(f"{path}.{key} maxLength")
                errors.extend(closed_schema_errors(sub, f"{path}.{key}"))
    elif schema.get("type") == "array":
        if "maxItems" not in schema:
            errors.append(f"{path} maxItems")
        items = schema.get("items")
        if isinstance(items, dict):
            errors.extend(closed_schema_errors(items, path + "[]"))
    return errors


def segment_ids(manifest: dict) -> list[str]:
    found: list[str] = []
    for rendition in manifest.get("renditions", []):
        for segment in rendition.get("segments", []):
            found.append(segment.get("segment_id", ""))
    return found


def check_contracts(bounds: dict) -> list[str]:
    errors: list[str] = []
    schemas = {}
    for name in (
        "media-manifest.schema.json",
        "provider-capability.schema.json",
        "evidence-envelope.schema.json",
        "public-record.schema.json",
    ):
        schema = json.loads((ROOT / "docs/protocol/schemas" / name).read_text(encoding="utf-8"))
        schemas[name] = schema
        closed = closed_schema_errors(schema, name)
        if closed:
            errors.append(f"{name} not closed: {closed}")
    manifest_schema = schemas["media-manifest.schema.json"]
    valid = json.loads((ROOT / "docs/protocol/fixtures/manifest-valid.json").read_text(encoding="utf-8"))
    if validate_schema(valid, manifest_schema) or len(segment_ids(valid)) != len(set(segment_ids(valid))):
        errors.append("valid manifest rejected")
    fault_files = {
        "manifest-invalid-boundary.json": "schema",
        "manifest-invalid-unknown.json": "schema",
        "manifest-invalid-wrong-domain.json": "schema",
        "manifest-invalid-duplicate-id.json": "duplicate",
    }
    for name, kind in fault_files.items():
        document = json.loads((ROOT / "docs/protocol/fixtures" / name).read_text(encoding="utf-8"))
        schema_errors = validate_schema(document, manifest_schema)
        duplicate = len(segment_ids(document)) != len(set(segment_ids(document)))
        if kind == "schema" and not schema_errors:
            errors.append(f"{name} was accepted")
        if kind == "schema" and duplicate:
            errors.append(f"{name} stacks a duplicate id")
        if kind == "duplicate" and schema_errors:
            errors.append(f"{name} fails schema before the duplicate-id check")
        if kind == "duplicate" and not duplicate:
            errors.append(f"{name} has no duplicate id")
    evidence = schemas["evidence-envelope.schema.json"]
    if "STORAGE_PROOF" in evidence["properties"]["kind"]["enum"]:
        errors.append("evidence schema allows a storage proof kind")
    client = json.loads((ROOT / "docs/protocol/api/client-api.json").read_text(encoding="utf-8"))
    provider = json.loads((ROOT / "docs/protocol/api/provider-api.json").read_text(encoding="utf-8"))
    for path in CLIENT_PATHS:
        if path not in client["paths"]:
            errors.append(f"client API missing {path}")
    if "/register" in client["paths"] or "/signup" in client["paths"]:
        errors.append("client API has a public sign-up route")
    for path in PROVIDER_PATHS:
        if path not in provider["paths"]:
            errors.append(f"provider API missing {path}")
    genesis = json.loads((ROOT / "docs/protocol/fixtures/genesis-valid.json").read_text(encoding="utf-8"))
    errors.extend(check_genesis(genesis, bounds, valid=True))
    minted = json.loads(json.dumps(genesis))
    minted["mint_module_enabled"] = True
    if not check_genesis(minted, bounds, valid=False):
        errors.append("mint genesis was accepted")
    approved = json.loads(json.dumps(genesis))
    approved["blocked_decisions"][0]["approved"] = True
    if not check_genesis(approved, bounds, valid=False):
        errors.append("approved decision genesis was accepted")
    return errors


def check_genesis(doc: dict, bounds: dict, valid: bool) -> list[str]:
    errors: list[str] = []
    for flag in (
        "mint_module_enabled",
        "distribution_module_enabled",
        "slashing_module_enabled",
        "staking_module_enabled",
        "permissionless_validator_creation",
    ):
        if doc.get(flag) is not False:
            errors.append(flag)
    if doc.get("protocol_version") != 1:
        errors.append("protocol_version")
    if doc.get("approved_council"):
        errors.append("approved_council")
    if doc.get("contents") or doc.get("leases") or doc.get("offers"):
        errors.append("nonempty catalogue")
    seen = {row.get("decision_id") for row in doc.get("blocked_decisions", [])}
    if seen != set(bounds["decisions"]):
        errors.append("decision set")
    for row in doc.get("blocked_decisions", []):
        if row.get("approved") or row.get("value_version") != 0 or row.get("schema_version") != 1:
            errors.append(f"{row.get('decision_id')} not blocked")
    for key, value in doc.get("safety_caps", {}).items():
        if bounds.get(key) != value:
            errors.append(f"safety {key}")
    pubs = [row.get("consensus_pubkey_hex") for row in doc.get("validators", [])]
    powers = [row.get("power") for row in doc.get("validators", [])]
    if len(pubs) != len(set(pubs)) or any(not isinstance(item, str) or len(item) != 64 for item in pubs):
        errors.append("validator keys")
    if powers and len(set(powers)) != 1:
        errors.append("validator powers")
    if doc.get("allow_devnet_test_parameters"):
        if not str(doc.get("chain_id", "")).startswith(bounds["devnet_chain_id_prefix"]):
            errors.append("devnet chain id")
        for row in doc.get("devnet_test_parameters", []):
            if row.get("label") != "PROPOSED_TEST_PARAMETER" or row.get("approved_policy"):
                errors.append("devnet test label")
    if "private_key" in json.dumps(doc):
        errors.append("private key")
    if valid and errors:
        return [f"valid genesis: {', '.join(errors)}"]
    if not valid:
        return errors
    return []


def python_vector_report(doc: dict) -> list[str]:
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

    lines: list[str] = []
    canonical: dict[str, str] = {}
    for item in doc["valid"]:
        got = jcs.parse_and_canonicalize(item["input_raw"])
        if got != item["canonical"]:
            raise AssertionError(item["id"])
        digest = hashlib.sha256(got.encode("utf-8")).hexdigest()
        lines.append(f"VALID {item['id']} {digest}")
        canonical[item["id"]] = got
    for item in doc["invalid"]:
        try:
            if item.get("raw_hex"):
                jcs.parse_bytes(bytes.fromhex(item["raw_hex"]))
            else:
                jcs.parse_and_canonicalize(item["raw"])
        except jcs.JCSError as exc:
            if exc.reason != item["reason"]:
                raise AssertionError(item["id"]) from exc
        else:
            raise AssertionError(item["id"])
        lines.append(f"INVALID {item['id']} {item['reason']}")
    safe_max = 9007199254740991
    uint64_max = 18446744073709551615
    for item in doc["integers"]:
        value = int(item["decimal"])
        safe = abs(value) <= safe_max
        fits = value >= 0 and value <= uint64_max
        if safe != item["safe"] or fits != item["uint64"]:
            raise AssertionError(item["id"])
        lines.append(f"INTEGER {item['id']} {'safe' if safe else 'unsafe'} {'uint64' if fits else 'over'}")
    for item in doc["arithmetic"]:
        left = int(item["a"])
        right = int(item["b"])
        if item["op"] == "add":
            result = "overflow" if left > uint64_max - right else str(left + right)
        else:
            result = "underflow" if left < right else str(left - right)
        if result != item["result"]:
            raise AssertionError(item["id"])
        lines.append(f"ARITH {item['id']} {result}")
    signer = Ed25519PrivateKey.from_private_bytes(bytes.fromhex(doc["rfc8032_seed"]))
    empty = signer.sign(b"").hex()
    if empty != doc["rfc8032_empty_signature"]:
        raise AssertionError("rfc8032")
    lines.append(f"RFC8032 {empty}")
    for item in doc["sign"]:
        body = canonical[item["vector_id"]].encode("utf-8")
        signed = item["domain"].encode("utf-8") + b"\x00" + body
        signature = signer.sign(signed).hex()
        lines.append(f"SIGN {item['id']} {hashlib.sha256(signed).hexdigest()} {signature}")
    return lines


def run_language(command: list[str]) -> tuple[int, str]:
    try:
        completed = subprocess.run(command, cwd=ROOT, capture_output=True, text=True, check=False)
    except FileNotFoundError:
        return 127, ""
    return completed.returncode, completed.stdout


def compare_languages(python_lines: list[str], go_text: str, ts_text: str) -> list[str]:
    """Require the three reports to be the same list, in both directions."""
    go_lines = [line for line in go_text.splitlines() if line]
    ts_lines = [line for line in ts_text.splitlines() if line]
    errors: list[str] = []
    if python_lines != go_lines:
        errors.append("python and go vector reports differ")
    if python_lines != ts_lines:
        errors.append("python and typescript vector reports differ")
    if go_lines != ts_lines:
        errors.append("go and typescript vector reports differ")
    return errors


def source_clock_errors() -> list[str]:
    errors: list[str] = []
    for name in ("spec_machine.py", "jcs.py", "cases.py"):
        text = (Path(__file__).resolve().parent / name).read_text(encoding="utf-8")
        for token in ("time.time", "datetime", "requests.", "urllib", "socket.", "random."):
            if token in text:
                errors.append(f"{name} contains {token}")
    return errors


def input_files() -> list[Path]:
    files = [
        PROTOCOL,
        PROGRESS,
        PROGRESS_TSV,
        DECISIONS,
        ROOT / "docs" / "requirements.md",
        ROOT / "docs" / "evidence" / "goal-01" / "check_requirements.py",
        Path(__file__).resolve(),
    ]
    files.extend(sorted((ROOT / "docs" / "protocol").rglob("*")))
    files.extend(sorted((ROOT / "docs" / "evidence" / "goal-02").rglob("*")))
    unique: list[Path] = []
    seen: set[Path] = set()
    for path in files:
        if path.is_file() and path not in seen and path.name != "goal-02.md" and "__pycache__" not in path.parts and path.suffix != ".pyc":
            seen.add(path)
            unique.append(path)
    return sorted(unique)


def main() -> int:
    errors: list[str] = []
    bounds = json.loads(BOUNDS_PATH.read_text(encoding="utf-8"))
    protocol = PROTOCOL.read_text(encoding="utf-8")
    decisions = DECISIONS.read_text(encoding="utf-8")
    errors.extend(check_text(protocol, decisions))
    errors.extend(check_protos(bounds))
    errors.extend(check_contracts(bounds))
    errors.extend(source_clock_errors())
    results = cases.cases()
    problems = [case_id for case_id, detail in results if detail]
    ran = [case_id for case_id, _detail in results]
    ran_set = set(ran)
    if len(ran) != len(ran_set):
        errors.append("duplicate transition case id")
    if ran_set != REQUIRED_CASES or len(results) != len(REQUIRED_CASES):
        missing = REQUIRED_CASES - ran_set
        extra = ran_set - REQUIRED_CASES
        errors.append(f"case pin missing {sorted(missing)} extra {sorted(extra)} count {len(results)}")
    if problems:
        errors.append("failed cases " + ", ".join(problems))
    print(f"transition_cases {len(results)}")
    print(f"transition_case_pin {len(REQUIRED_CASES)}")
    print(f"transition_failures {len(problems)}")
    doc = json.loads(VECTORS.read_text(encoding="utf-8"))
    valid_ids = {item["id"] for item in doc["valid"]}
    invalid_ids = {item["id"] for item in doc["invalid"]}
    if valid_ids != REQUIRED_VALID_IDS or len(doc["valid"]) != len(REQUIRED_VALID_IDS):
        errors.append(f"valid vector pin {len(doc['valid'])}")
    if invalid_ids != REQUIRED_INVALID_IDS or len(doc["invalid"]) != len(REQUIRED_INVALID_IDS):
        errors.append(f"invalid vector pin {len(doc['invalid'])}")
    if len(doc["integers"]) != 6 or len(doc["arithmetic"]) != 5:
        errors.append("integer or arithmetic vector count")
    try:
        python_lines = python_vector_report(doc)
    except (AssertionError, jcs.JCSError, KeyError, ValueError) as exc:
        errors.append(f"python vectors {exc}")
        python_lines = []
    go_code, go_out = run_language(["go", "run", str(ROOT / "docs/evidence/goal-02/go/vectorcheck.go"), str(VECTORS)])
    ts_code, ts_out = run_language(
        ["node", "--experimental-strip-types", str(ROOT / "docs/evidence/goal-02/ts/vectorcheck.ts"), str(VECTORS)]
    )
    print(f"go_vectors exit:{go_code}")
    print(f"typescript_vectors exit:{ts_code}")
    languages_match = False
    if go_code == 127:
        print("go BLOCKED")
        errors.append("go BLOCKED")
    elif go_code != 0:
        errors.append(f"go exit {go_code}")
    if ts_code == 127:
        print("typescript BLOCKED")
        errors.append("typescript BLOCKED")
    elif ts_code != 0:
        errors.append(f"typescript exit {ts_code}")
    if go_code == 0 and ts_code == 0 and python_lines:
        language_errors = compare_languages(python_lines, go_out, ts_out)
        errors.extend(language_errors)
        languages_match = not language_errors
    print(f"valid_vectors {len(doc['valid'])}")
    print(f"invalid_vectors {len(doc['invalid'])}")
    print(f"integer_vectors {len(doc['integers'])}")
    print(f"arithmetic_vectors {len(doc['arithmetic'])}")
    print("hashes")
    for path in input_files():
        print(f"{sha256_file(path)}  {path.relative_to(ROOT).as_posix()}")
    print(f"errors {len(errors)}")
    for error in errors:
        print(f"ERROR {error}")
    if errors:
        print("result FAIL")
        return 1
    if languages_match:
        print("languages MATCH")
    print("result PASS")
    print("note schema and fixture check; protocol not approved; decisions OPEN; public launch disabled")
    return 0


if __name__ == "__main__":
    sys.exit(main())
