"""Evidence envelope fixtures against the frozen envelope schema.

REQ-EVID-001: receipts and audits are labelled as attestations, and storage-proof,
unique-replica, independence, legality, and future-quality labels are rejected.
REQ-EVID-007: a Merkle inclusion presented as proof of a unique replica is rejected.

Every invalid fixture is a valid base envelope with stated changes. The tests check
that the schema rejects it with exactly the errors `cases.json` states, and that the
fixture differs from its base only at the keys those errors name.
"""

from __future__ import annotations

import hashlib
import importlib.util
import json
import re
from collections import Counter
from types import ModuleType
from typing import Any

import pytest
from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
from jsonschema.exceptions import ValidationError

from svn_protocol.schemas import repo_root, schema_path, validator_for

ROOT = repo_root()
FIXTURES = ROOT / "tests" / "fixtures" / "evidence"
# Written by the Go producer (agents/auditor/batch TestGoldenEnvelopeFixtures),
# listed by its own index.json, not by cases.json.
GOLDEN = FIXTURES / "golden"
SCHEMA_NAME = "evidence-envelope.schema.json"
STATE_PROTO = ROOT / "docs" / "protocol" / "proto" / "svn" / "evidence" / "v1" / "state.proto"
KIND_PREFIX = "EVIDENCE_KIND_"
RECEIPT = "PROVIDER_RECEIPT"
AUDIT = "AUDITOR_ATTESTATION"
CLAIM_LIMIT = "ATTESTATION_NOT_STORAGE_PROOF"

LABEL_GROUPS = (
    "storage-proof-label",
    "unique-replica-label",
    "independence-label",
    "legality-label",
    "future-quality-label",
)
MERKLE_GROUP = "merkle-inclusion-as-unique-replica"
GROUPS = frozenset(
    {
        *LABEL_GROUPS,
        MERKLE_GROUP,
        "attestation-label-removed",
        "unknown-field",
        "wrong-kind",
        "reserved-kind",
        "bounds",
    }
)
REQUIREMENTS = frozenset({"REQ-EVID-001", "REQ-EVID-007"})
ROOT_KEYWORDS = frozenset({"additionalProperties", "required"})
BOUND_KEYWORDS = {"minimum": -1, "maximum": 1, "minLength": -1, "maxLength": 1}

ErrorKey = tuple[str, str, tuple[str, ...]]


def _unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate key {key!r}")
        result[key] = value
    return result


def _no_fraction(text: str) -> Any:
    raise ValueError(f"number {text} is not an integer")


def _no_constant(text: str) -> Any:
    raise ValueError(f"{text} is not JSON")


def strict_loads(text: str) -> Any:
    """`json.loads` that refuses duplicate keys, fractions, exponents, NaN, and Infinity.

    The standard loader keeps the last duplicate key, and the schema's integer type
    accepts 1.0, so either would let a fixture mean something its bytes do not say.
    """
    return json.loads(
        text,
        object_pairs_hook=_unique_object,
        parse_float=_no_fraction,
        parse_constant=_no_constant,
    )


def _load(relative: str) -> Any:
    return strict_loads((FIXTURES / relative).read_text(encoding="utf-8"))


INDEX: dict[str, Any] = _load("cases.json")
VALID: list[dict[str, Any]] = INDEX["valid"]
INVALID: list[dict[str, Any]] = INDEX["invalid"]
SCHEMA: dict[str, Any] = strict_loads(schema_path(SCHEMA_NAME).read_text(encoding="utf-8"))
PROPERTIES: dict[str, Any] = SCHEMA["properties"]
VALIDATOR = validator_for(SCHEMA_NAME)


def _file_ids(cases: list[dict[str, Any]]) -> list[str]:
    return [case["file"] for case in cases]


def _group(name: str) -> list[dict[str, Any]]:
    return [case for case in INVALID if case["group"] == name]


def _pointer(error: ValidationError) -> str:
    return "".join(f"/{part}" for part in error.absolute_path)


def _actual_errors(document: Any) -> Counter[ErrorKey]:
    found: Counter[ErrorKey] = Counter()
    for error in VALIDATOR.iter_errors(document):
        keyword = error.validator
        assert isinstance(keyword, str)
        names: tuple[str, ...] = ()
        if keyword == "additionalProperties":
            assert isinstance(error.instance, dict)
            names = tuple(sorted(set(error.instance) - set(PROPERTIES)))
        elif keyword == "required":
            assert isinstance(error.instance, dict)
            assert isinstance(error.validator_value, list)
            names = tuple(sorted(set(error.validator_value) - set(error.instance)))
        found[(_pointer(error), keyword, names)] += 1
    return found


def _stated_errors(case: dict[str, Any]) -> Counter[ErrorKey]:
    stated: Counter[ErrorKey] = Counter()
    for entry in case["errors"]:
        names = tuple(entry.get("names", ()))
        # jsonschema reports one `required` error per missing property.
        count = len(names) if entry["keyword"] == "required" else 1
        stated[(entry["path"], entry["keyword"], names)] += count
    return stated


def _stated_keys(case: dict[str, Any]) -> set[str]:
    keys: set[str] = set()
    for entry in case["errors"]:
        if entry["path"] == "":
            assert entry["keyword"] in ROOT_KEYWORDS, entry
            assert entry["names"], entry
            keys.update(entry["names"])
        else:
            key = entry["path"].removeprefix("/")
            assert key in PROPERTIES and "/" not in key, entry
            keys.add(key)
    return keys


def _encoded(value: Any) -> str:
    # json.dumps keeps true and 1 apart, which == does not.
    return json.dumps(value, sort_keys=True)


def _changed_keys(base: dict[str, Any], document: dict[str, Any]) -> set[str]:
    return {
        key
        for key in set(base) | set(document)
        if key not in base or key not in document or _encoded(base[key]) != _encoded(document[key])
    }


def _evidence_kind_enum() -> tuple[dict[str, int], set[int], set[str]]:
    text = STATE_PROTO.read_text(encoding="utf-8")
    block = re.search(r"enum EvidenceKind \{(?P<body>[^}]*)\}", text)
    assert block is not None, "EvidenceKind enum not found in state.proto"
    body = block.group("body")
    values = {
        name: int(number)
        for name, number in re.findall(r"\b(EVIDENCE_KIND_[A-Z_]+) = (\d+);", body)
    }
    numbers = re.search(r"\breserved (\d+(?:, \d+)*);", body)
    assert numbers is not None, "EvidenceKind reserved numbers not found"
    reserved_numbers = {int(number) for number in numbers.group(1).split(", ")}
    reserved_names = set(re.findall(r'"(EVIDENCE_KIND_[A-Z_]+)"', body))
    assert values and reserved_names
    return values, reserved_numbers, reserved_names


def _kinds_in(groups: tuple[str, ...]) -> list[Any]:
    return [_load(case["file"]).get("kind") for case in INVALID if case["group"] in groups]


def test_strict_loader_refuses_ambiguous_json() -> None:
    for text in (
        '{"kind": 1, "kind": 2}',
        '{"content_version": 1.0}',
        '{"epoch": 1e3}',
        '{"epoch": NaN}',
        '{"epoch": Infinity}',
    ):
        with pytest.raises(ValueError):
            strict_loads(text)
    assert strict_loads('{"kind": null, "epoch": 0}') == {"kind": None, "epoch": 0}


def test_index_names_the_frozen_envelope_schema() -> None:
    assert set(INDEX) == {"schema", "valid", "invalid"}
    assert INDEX["schema"] == SCHEMA_NAME
    assert SCHEMA["$id"] == "svn-evidence-envelope-v1"
    assert VALIDATOR.schema == SCHEMA


def test_every_fixture_file_is_indexed_once() -> None:
    on_disk = {
        path.relative_to(FIXTURES).as_posix()
        for path in FIXTURES.rglob("*")
        if path.is_file()
        and path.suffix == ".json"
        and path.name != "cases.json"
        and GOLDEN not in path.parents
    }
    valid = _file_ids(VALID)
    invalid = _file_ids(INVALID)
    assert len(valid + invalid) == len(set(valid + invalid))
    assert all(name.startswith("valid/") for name in valid)
    assert all(name.startswith("invalid/") for name in invalid)
    assert set(valid + invalid) == on_disk


@pytest.mark.parametrize("case", VALID, ids=_file_ids(VALID))
def test_valid_envelope_is_accepted(case: dict[str, Any]) -> None:
    document = _load(case["file"])
    assert _actual_errors(document) == Counter()
    assert document["attestation"] is True
    assert document["claim_limit"] == CLAIM_LIMIT
    assert document["kind"] in (RECEIPT, AUDIT)
    assert "REQ-EVID-001" in case["requirements"]
    assert set(case["requirements"]) <= REQUIREMENTS
    assert case["note"]


def test_valid_fixtures_cover_every_kind() -> None:
    kinds = {_load(case["file"])["kind"] for case in VALID}
    assert kinds == set(PROPERTIES["kind"]["enum"]) == {RECEIPT, AUDIT}


@pytest.mark.parametrize("case", INVALID, ids=_file_ids(INVALID))
def test_invalid_envelope_is_rejected_for_the_stated_reason(case: dict[str, Any]) -> None:
    assert case["group"] in GROUPS
    assert case["reason"]
    assert set(case["requirements"]) <= REQUIREMENTS
    assert case["errors"]
    document = _load(case["file"])
    assert not VALIDATOR.is_valid(document)
    assert _actual_errors(document) == _stated_errors(case)


@pytest.mark.parametrize("case", INVALID, ids=_file_ids(INVALID))
def test_invalid_fixture_differs_from_its_base_only_where_stated(case: dict[str, Any]) -> None:
    assert case["base"] in _file_ids(VALID)
    base = _load(case["base"])
    document = _load(case["file"])
    assert isinstance(document, dict)
    assert _changed_keys(base, document) == _stated_keys(case)


def test_every_group_has_fixtures() -> None:
    assert {case["group"] for case in INVALID} == GROUPS


def test_label_groups_cover_receipts_and_audits() -> None:
    for group in LABEL_GROUPS:
        cases = _group(group)
        bases = {_load(case["base"])["kind"] for case in cases}
        assert bases == {RECEIPT, AUDIT}, group
        assert all("REQ-EVID-001" in case["requirements"] for case in cases), group


def test_attestation_label_cannot_be_removed() -> None:
    cases = _group("attestation-label-removed")
    keys: set[str] = set()
    for case in cases:
        keys |= _stated_keys(case)
    assert keys == {"attestation", "claim_limit"}
    bases = {_load(case["base"])["kind"] for case in cases}
    assert bases == {RECEIPT, AUDIT}
    assert all("REQ-EVID-001" in case["requirements"] for case in cases)


def test_merkle_inclusion_presented_as_unique_replica_proof_is_rejected() -> None:
    cases = _group(MERKLE_GROUP)
    assert cases
    claimed_in: set[str] = set()
    for case in cases:
        assert {"REQ-EVID-001", "REQ-EVID-007"} <= set(case["requirements"])
        assert {"merkle_root", "inclusion_proof"} <= _stated_keys(case)
        document = _load(case["file"])
        # The fixture names its own observation digest as the leaf. Nothing here
        # computes or checks the inclusion: no Merkle construction is written, and
        # the schema rejects the fixture for its fields and labels alone.
        assert document["inclusion_proof"]["leaf_sha256"] == document["observation_sha256"]
        assert not VALIDATOR.is_valid(document)
        for key in ("kind", "claim_limit"):
            if document[key] == "UNIQUE_REPLICA_PROOF":
                claimed_in.add(key)
    assert claimed_in == {"kind", "claim_limit"}


def test_proto_kinds_match_the_schema_and_reserved_kinds_are_rejected() -> None:
    values, reserved_numbers, reserved_names = _evidence_kind_enum()
    zero = [name for name, number in values.items() if number == 0]
    assert zero == [KIND_PREFIX + "UNSPECIFIED"]
    named = sorted(name.removeprefix(KIND_PREFIX) for name, number in values.items() if number)
    assert named == sorted(PROPERTIES["kind"]["enum"])

    reserved = _kinds_in(("reserved-kind",))
    reserved_ints = {kind for kind in reserved if type(kind) is int}
    assert reserved_ints == reserved_numbers
    assert {kind for kind in reserved if isinstance(kind, str)} == reserved_names

    labelled = [kind for kind in _kinds_in(LABEL_GROUPS) if kind not in (RECEIPT, AUDIT)]
    for name in reserved_names:
        assert name.removeprefix(KIND_PREFIX) in labelled, name

    wrong = _kinds_in(("wrong-kind",))
    for number in values.values():
        assert any(type(kind) is int and kind == number for kind in wrong), number
    assert KIND_PREFIX + "UNSPECIFIED" in wrong

    for kind in [*reserved, *labelled, *wrong]:
        document = dict(_load("valid/auditor-attestation.json"), kind=kind)
        assert not VALIDATOR.is_valid(document), kind


def test_every_envelope_property_has_a_negative_fixture() -> None:
    assert sorted(SCHEMA["required"]) == sorted(PROPERTIES)
    targeted: set[str] = set()
    for case in INVALID:
        targeted |= _stated_keys(case)
    assert set(PROPERTIES) <= targeted


def test_bound_fixtures_are_one_step_past_an_accepted_value() -> None:
    accepted = [_load(case["file"]) for case in VALID]
    checked = 0
    for case in _group("bounds"):
        document = _load(case["file"])
        for entry in case["errors"]:
            step = BOUND_KEYWORDS.get(entry["keyword"])
            if step is None:
                continue
            key = entry["path"].removeprefix("/")
            limit = PROPERTIES[key][entry["keyword"]]
            if entry["keyword"] in ("minLength", "maxLength"):
                assert len(document[key]) == limit + step, case["file"]
                assert any(len(valid[key]) == limit for valid in accepted), case["file"]
            else:
                assert document[key] == limit + step, case["file"]
                assert any(valid[key] == limit for valid in accepted), case["file"]
            checked += 1
    assert checked >= len(BOUND_KEYWORDS)


SIGNATURE_DOMAIN = b"SVN-EVIDENCE-ATTESTATION-1"
GOLDEN_UPDATE = "go test ./batch -run TestGoldenEnvelopeFixtures -update (in agents/auditor)"


def _goal02_jcs() -> ModuleType:
    path = ROOT / "docs" / "evidence" / "goal-02" / "jcs.py"
    spec = importlib.util.spec_from_file_location("goal02_jcs", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _canonical_text(jcs: ModuleType, raw: bytes) -> str:
    text = raw.decode("utf-8")
    assert jcs.parse_and_canonicalize(text) == text, "not canonical SVN-JCS-1"
    return text


def test_golden_envelopes_from_the_go_producer() -> None:
    """Cross-language check of the Go producer's envelopes (plan 21.4).

    The Go test writes the files; this test fails, never skips, when they are
    missing. Python re-derives everything the Go side claims: schema validity,
    canonical form, the envelopes inside the signed payload, the ed25519
    signature, the signer address, the evidence ids, and the digest rule.
    """
    index_path = GOLDEN / "index.json"
    assert index_path.is_file(), f"no golden producer output in {GOLDEN}: run {GOLDEN_UPDATE}"
    jcs = _goal02_jcs()
    index = strict_loads(index_path.read_text(encoding="utf-8"))
    items = index["items"]
    assert {item["result"] for item in items} == {"PASS", "FAIL"}

    listed = (
        {"index.json"} | {item["envelope"] for item in items} | {item["report"] for item in items}
    )
    on_disk = {
        path.relative_to(GOLDEN).as_posix()
        for path in GOLDEN.rglob("*")
        if path.is_file() and path.name != "README.md"
    }
    assert on_disk == listed

    public = bytes.fromhex(index["public_key"])
    signer = hashlib.sha256(public).digest()[:20].hex()
    assert index["signer"] == signer

    canonical = index["canonical"]
    assert jcs.parse_and_canonicalize(canonical) == canonical
    payload = strict_loads(canonical)
    assert payload["signer"] == signer
    assert payload["batch_id"] == index["batch_id"]
    assert payload["batch_id"].startswith(signer[:16] + ".")
    key = Ed25519PublicKey.from_public_bytes(public)
    signature = bytes.fromhex(index["signature"])
    message = SIGNATURE_DOMAIN + b"\x00" + canonical.encode("utf-8")
    key.verify(signature, message)
    altered = message.replace(b'"epoch":', b'"epoch":1', 1)
    with pytest.raises(InvalidSignature):
        key.verify(signature, altered)

    assert len(payload["envelopes"]) == len(items)
    for item, signed in zip(items, payload["envelopes"], strict=True):
        raw = (GOLDEN / item["envelope"]).read_bytes()
        document = strict_loads(_canonical_text(jcs, raw))
        assert _actual_errors(document) == Counter(), item["envelope"]
        assert document == signed
        assert jcs.canonicalize(signed).encode("utf-8") == raw
        assert document["epoch"] == index["epoch"]
        assert document["evidence_id"] == item["evidence_id"]
        assert item["envelope"] == f"envelopes/{item['evidence_id']}.json"

        report_raw = (GOLDEN / item["report"]).read_bytes()
        report = strict_loads(_canonical_text(jcs, report_raw))
        digest = hashlib.sha256(report_raw).hexdigest()
        assert item["report"] == f"reports/{digest}.json"
        assert document["evidence_id"] == f"{signer[:16]}.{digest[:47]}"
        assert report["result"] == item["result"]
        assert report["auditor_address"] == signer
        assert report["auditor_key_id"] == document["signer_key_id"]
        assert report["claim_limit"] == document["claim_limit"] == CLAIM_LIMIT
        for key_name in ("lease_id", "content_id", "content_version"):
            assert report[key_name] == document[key_name], key_name
        assert document["observation_sha256"] == item["observation_sha256"]
        if item["result"] == "FAIL":
            assert document["observation_sha256"] == digest
        else:
            assert document["observation_sha256"] == report["expected_manifest_sha256"] != digest
