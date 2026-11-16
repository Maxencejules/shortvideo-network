"""Cross-language check of the public-record history fixture."""

from __future__ import annotations

import hashlib
import importlib.util
import json
from pathlib import Path

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey

ROOT = Path(__file__).resolve().parents[2]
FIXTURE = ROOT / "services" / "api" / "internal" / "records" / "testdata" / "history.json"
JCS = ROOT / "docs" / "evidence" / "goal-02"


def test_public_record_history_fixture() -> None:
    spec = importlib.util.spec_from_file_location("goal02_jcs", JCS / "jcs.py")
    if spec is None or spec.loader is None:
        raise RuntimeError(JCS / "jcs.py")
    jcs = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(jcs)
    doc = json.loads(FIXTURE.read_text(encoding="utf-8"))
    assert doc["domain"] == "SVN-PUBLIC-RECORD-1"
    pubs = {item["key_id"]: bytes.fromhex(item["public"]) for item in doc["keys"]}
    previous: str | None = None
    for record in doc["records"]:
        parsed = json.loads(record["canonical"])
        assert jcs.canonicalize(parsed) == record["canonical"]
        Ed25519PublicKey.from_public_bytes(pubs[record["key_id"]]).verify(
            bytes.fromhex(record["signature"]),
            b"SVN-PUBLIC-RECORD-1\x00" + record["canonical"].encode(),
        )
        if previous is not None:
            assert parsed["previous_digest"] == previous
        previous = hashlib.sha256(record["canonical"].encode()).hexdigest()
    assert doc["records"][0]["key_id"] == "k-old"
    assert doc["records"][1]["key_id"] == "k-new"
    assert doc["records"][2]["key_id"] == "k-old"
