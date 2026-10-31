"""Validate documents against the frozen schema files.

The schema bytes live in `docs/protocol/schemas`. This package does not
copy them and it does not define a second manifest format.
"""

from __future__ import annotations

import json
from pathlib import Path

from jsonschema import Draft202012Validator
from jsonschema.exceptions import ValidationError

SCHEMA_DIR_NAME = "media-manifest.schema.json"


class SchemaError(ValueError):
    """The document does not match the frozen schema."""


def repo_root() -> Path:
    here = Path(__file__).resolve()
    for parent in here.parents:
        candidate = parent / "docs" / "protocol" / "schemas"
        if candidate.is_dir():
            return parent
    raise RuntimeError("frozen schema directory not found")


def schema_path(name: str) -> Path:
    path = repo_root() / "docs" / "protocol" / "schemas" / name
    if not path.is_file():
        raise FileNotFoundError(path)
    return path


def validator_for(name: str) -> Draft202012Validator:
    schema = json.loads(schema_path(name).read_text(encoding="utf-8"))
    Draft202012Validator.check_schema(schema)
    return Draft202012Validator(schema)


def validate_manifest(document: object) -> None:
    """Raise SchemaError when `document` is not a v0 media manifest."""
    validator = validator_for(SCHEMA_DIR_NAME)
    errors = sorted(validator.iter_errors(document), key=lambda item: list(item.path))
    if errors:
        first: ValidationError = errors[0]
        location = "/".join(str(part) for part in first.path) or "$"
        raise SchemaError(f"{location}: {first.message}")
