"""The protocol package validates frozen manifest fixtures."""

from __future__ import annotations

import json

import pytest

from svn_protocol.schemas import SchemaError, repo_root, validate_manifest

ROOT = repo_root()
FIXTURES = ROOT / "docs" / "protocol" / "fixtures"


def _load(name: str) -> object:
    return json.loads((FIXTURES / name).read_text(encoding="utf-8"))


def test_valid_manifest_matches_the_frozen_schema() -> None:
    validate_manifest(_load("manifest-valid.json"))


def test_unknown_field_is_rejected() -> None:
    with pytest.raises(SchemaError, match="video_bytes"):
        validate_manifest(_load("manifest-invalid-unknown.json"))


def test_boundary_violation_is_rejected() -> None:
    with pytest.raises(SchemaError):
        validate_manifest(_load("manifest-invalid-boundary.json"))


def test_schema_files_stay_in_the_frozen_tree() -> None:
    names = sorted(path.name for path in (ROOT / "docs" / "protocol" / "schemas").glob("*.json"))
    assert names == [
        "evidence-envelope.schema.json",
        "media-manifest.schema.json",
        "provider-capability.schema.json",
        "public-record.schema.json",
    ]
