"""Council payload vectors (chain/x/authority/testdata/payload-vectors.json).

The Go test TestPayloadVectors checks the same file against the chain code.
This test checks it against goal 02's reference code instead, so the vectors
are not checked only by the code that produced them: docs/evidence/goal-02/
jcs.py recomputes each canonical text, spec_machine.payload_digest each
digest, and spec_machine.validated_payload (the propose-time payload check of
the frozen spec) accepts each payload with its parameter key.

D-1: the chain refuses a governed limit outside bounds.json
governed_limit_caps at propose (chain_propose_code ERROR_CODE_BOUNDS). The
spec accepts those payloads at propose and refuses them at execute
(apply_proposal_payload returns ERROR_CODE_BOUNDS). The test asserts both
halves, so the vector file records the deviation exactly.
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
BOUNDS_CODE = "ERROR_CODE_BOUNDS"


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
    assert {vector["kind"] for vector in vectors} == V0_KINDS
    refused = 0
    reordered = 0
    for vector in vectors:
        name = vector["id"]
        payload = json.loads(vector["payload_json"])
        canonical = vector["canonical"]
        reordered += vector["payload_json"] != canonical
        assert jcs.parse_and_canonicalize(vector["payload_json"]) == canonical, name
        assert jcs.canonicalize(payload) == canonical, name
        digest = hashlib.sha256(canonical.encode("utf-8")).hexdigest()
        assert digest == vector["sha256"], name
        assert spec.payload_digest(payload) == vector["sha256"], name
        step = {
            "payload": payload,
            "payload_digest": vector["sha256"],
            "parameter_key": vector["parameter_key"],
        }
        state = _state(spec)
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


def test_every_governed_key_at_floor_and_ceiling() -> None:
    spec = _load("spec_machine")
    caps = spec.BOUNDS["governed_limit_caps"]
    accepted = {
        (
            json.loads(vector["canonical"])["governedLimit"]["limitKey"],
            int(json.loads(vector["canonical"])["governedLimit"]["value"]),
        )
        for vector in _vectors()
        if vector["kind"] == "governed_limit" and vector["chain_propose_code"] == ""
    }
    for key, ceiling in caps.items():
        assert (key, 1) in accepted, key
        assert (key, int(ceiling)) in accepted, key
