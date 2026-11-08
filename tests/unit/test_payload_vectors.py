"""Council payload vectors (chain/x/authority/testdata/payload-vectors.json).

The Go test TestPayloadVectors checks the same file against the chain code.
This test checks it against code that did not produce it: docs/evidence/
goal-02/jcs.py recomputes each canonical text and spec_machine.payload_digest
each digest; the generated Python protobuf module (proto/gen/python, from
docs/protocol/proto) parses each payload_json strictly as a
svn.authority.v1.ProposalPayload and its proto3 JSON mapping, canonicalized,
must give the same text; and spec_machine.validated_payload (the
propose-time payload check of the frozen spec) accepts each v0 payload with
its parameter key.

D-1: the chain refuses a governed limit outside bounds.json
governed_limit_caps at propose (chain_propose_code ERROR_CODE_BOUNDS). The
spec accepts those payloads at propose and refuses them at execute
(apply_proposal_payload returns ERROR_CODE_BOUNDS). The test asserts both
halves, so the vector file records the deviation exactly.

D-5: the schema version 2 payloads of the goal 10 revision (owner ruling Q1
"Minimal additive (Recommended)"; docs/protocol.md "Goal 10 additive
revision") are a governed limit with an activation height, software_upgrade,
cancel_upgrade and council_seat. The frozen spec parses every one of them as
ERROR_CODE_MALFORMED; the chain accepts them. The test asserts the spec
refusal, the parameter-key binding the revision states, and that a v0
payload's canonical text never carries activationHeight.
"""

from __future__ import annotations

import copy
import hashlib
import importlib.util
import json
import sys
from pathlib import Path
from types import ModuleType
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
GOAL02 = ROOT / "docs" / "evidence" / "goal-02"
VECTORS = ROOT / "chain" / "x" / "authority" / "testdata" / "payload-vectors.json"
FIELDS = {
    "id",
    "kind",
    "parameter_key",
    "chain_propose_code",
    "payload_json",
    "canonical",
    "sha256",
}
V0_KINDS = {
    "validator_power",
    "governed_limit",
    "admit_provider",
    "disable_provider",
    "force_withdraw",
}
V2_KINDS = {"software_upgrade", "cancel_upgrade", "council_seat"}
BOUNDS_CODE = "ERROR_CODE_BOUNDS"
MALFORMED_CODE = "ERROR_CODE_MALFORMED"


def _load(name: str) -> ModuleType:
    """Load one goal-02 module by path. spec_machine.py imports jcs by name,
    so the goal-02 directory is on sys.path while it loads."""
    path = GOAL02 / f"{name}.py"
    spec = importlib.util.spec_from_file_location(f"goal02_{name}", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(path)
    module = importlib.util.module_from_spec(spec)
    sys.path.insert(0, str(GOAL02))
    try:
        spec.loader.exec_module(module)
    finally:
        sys.path.remove(str(GOAL02))
    return module


def _vectors() -> list[dict[str, Any]]:
    document = json.loads(VECTORS.read_text(encoding="utf-8"))
    assert set(document) == {"description", "vectors"}
    vectors = document["vectors"]
    assert isinstance(vectors, list) and vectors
    for vector in vectors:
        assert set(vector) == FIELDS, vector.get("id")
    return vectors


def _proto3_canonical(jcs: ModuleType, payload_json: str) -> str:
    """Parse payload_json strictly with the generated Python protobuf module
    and return SVN-JCS-1 of its proto3 JSON mapping (lowerCamelCase names,
    64-bit integers as decimal strings, enum names, standard base64 bytes,
    zero values omitted). The protobuf runtime has no type stubs in the pinned
    requirements, so it is imported as a plain module."""
    json_format = importlib.import_module("google.protobuf.json_format")
    from svn.authority.v1 import tx_pb2

    message = json_format.Parse(payload_json, getattr(tx_pb2, "ProposalPayload")())
    return str(jcs.canonicalize(json_format.MessageToDict(message)))


def _schema2(vector: dict[str, Any]) -> bool:
    """A schema version 2 row: a new kind, or a governed limit that carries an
    activation height."""
    if vector["kind"] in V2_KINDS:
        return True
    canonical = json.loads(vector["canonical"])
    return "activationHeight" in canonical.get("governedLimit", {})


def _bound_key(vector: dict[str, Any]) -> str:
    """The parameter key the goal 10 revision binds a schema version 2
    payload to."""
    body = json.loads(vector["canonical"])
    if "governedLimit" in body:
        return str(body["governedLimit"]["limitKey"])
    if "councilSeat" in body:
        return "seat-" + str(body["councilSeat"]["seatId"])
    assert set(body) <= {"softwareUpgrade", "cancelUpgrade"}, body
    return "upgrade"


def _state(spec: ModuleType) -> dict[str, Any]:
    # validated_payload reads height and chain_time (activation in the
    # future); apply_proposal_payload reads governed_limits for a limit.
    return {"height": 1, "chain_time": spec.CHAIN_TIME, "governed_limits": {}}


def test_vectors_recompute_with_goal02_reference() -> None:
    jcs = _load("jcs")
    spec = _load("spec_machine")
    caps = spec.BOUNDS["governed_limit_caps"]
    vectors = _vectors()
    assert len({vector["id"] for vector in vectors}) == len(vectors)
    assert {vector["kind"] for vector in vectors} == V0_KINDS | V2_KINDS
    refused = 0
    reordered = 0
    schema2 = 0
    for vector in vectors:
        name = vector["id"]
        payload = json.loads(vector["payload_json"])
        canonical = vector["canonical"]
        reordered += vector["payload_json"] != canonical
        assert jcs.parse_and_canonicalize(vector["payload_json"]) == canonical, name
        assert jcs.canonicalize(payload) == canonical, name
        assert _proto3_canonical(jcs, vector["payload_json"]) == canonical, name
        digest = hashlib.sha256(canonical.encode("utf-8")).hexdigest()
        assert digest == vector["sha256"], name
        assert spec.payload_digest(payload) == vector["sha256"], name
        step = {
            "payload": payload,
            "payload_digest": vector["sha256"],
            "parameter_key": vector["parameter_key"],
        }
        state = _state(spec)
        if _schema2(vector):
            # D-5: the frozen spec does not know the schema version 2 payloads.
            schema2 += 1
            assert spec.validated_payload(state, step)["error"] == MALFORMED_CODE, name
            assert vector["parameter_key"] == _bound_key(vector), name
            assert vector["chain_propose_code"] == "", name
            continue
        assert "activationHeight" not in canonical, name
        normalized = spec.validated_payload(state, step)
        assert "error" not in normalized, f"{name}: spec refused {normalized}"
        assert normalized["kind"] == vector["kind"], name
        wrong_key = dict(step, parameter_key=vector["parameter_key"] + "x")
        assert spec.validated_payload(state, wrong_key)["error"] == "ERROR_CODE_MALFORMED", name
        wrong_digest = dict(step, payload_digest="00" * 32)
        assert spec.validated_payload(state, wrong_digest)["error"] == "ERROR_CODE_INTEGRITY", name
        if vector["kind"] != "governed_limit":
            assert vector["chain_propose_code"] == "", name
            continue
        key, value = normalized["limit_key"], normalized["value"]
        in_caps = key in caps and 1 <= value <= int(caps[key])
        executed = spec.apply_proposal_payload(copy.deepcopy(state), {"payload": normalized})
        if in_caps:
            assert vector["chain_propose_code"] == "", name
            assert executed is None, name
        else:
            assert vector["chain_propose_code"] == BOUNDS_CODE, name
            assert executed == BOUNDS_CODE, name
            refused += 1
    assert refused > 0
    assert reordered > 0, "no vector exercises key ordering"
    assert schema2 >= len(V2_KINDS) + 1, "a schema 2 kind or a scheduled limit has no vector"


def test_every_governed_key_at_floor_and_ceiling() -> None:
    spec = _load("spec_machine")
    caps = spec.BOUNDS["governed_limit_caps"]
    accepted = {
        (
            json.loads(vector["canonical"])["governedLimit"]["limitKey"],
            int(json.loads(vector["canonical"])["governedLimit"]["value"]),
        )
        for vector in _vectors()
        if vector["kind"] == "governed_limit"
        and vector["chain_propose_code"] == ""
        and not _schema2(vector)
    }
    for key, ceiling in caps.items():
        assert (key, 1) in accepted, key
        assert (key, int(ceiling)) in accepted, key
