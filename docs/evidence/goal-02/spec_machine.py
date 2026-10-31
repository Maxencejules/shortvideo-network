"""Executable specification of protocol v0 transitions.

This is not a consensus implementation and it is not a chain. It checks the
invariants Goal 02 freezes for later modules. It does not read a clock, the
network, or the filesystem except for the bounds file imported below.
DEC-001 through DEC-008 cannot be marked approved by any transition.
"""

from __future__ import annotations

import base64
import binascii
import copy
import hashlib
import json
import re
from pathlib import Path

from jcs import canonicalize

ROOT = Path(__file__).resolve().parents[3]
BOUNDS = json.loads((ROOT / "docs" / "protocol" / "bounds.json").read_text(encoding="utf-8"))

UINT64_MAX = int(BOUNDS["uint64_max"])
SAFE_MAX = int(BOUNDS["safe_integer_max"])
ID_RE = re.compile(BOUNDS["id_pattern"])
CHAIN_TIME = 1_700_000_000
PACKAGE_BYTES = 1000
TERM_SECONDS = 3600
MANIFEST = hashlib.sha256(b"svn-manifest-v1").hexdigest()
TERMS = hashlib.sha256(b"svn-terms-v1").hexdigest()
NOTE_A = hashlib.sha256(b"note-a").hexdigest()
NOTE_B = hashlib.sha256(b"note-b").hexdigest()

HOSTING = BOUNDS["domains"]["SIGNATURE_DOMAIN_HOSTING_INTENT"]
ACCEPT_DOMAIN = BOUNDS["domains"]["SIGNATURE_DOMAIN_PROVIDER_ACCEPT"]
RENEW_DOMAIN = BOUNDS["domains"]["SIGNATURE_DOMAIN_PROVIDER_RENEW"]
END_DOMAIN = BOUNDS["domains"]["SIGNATURE_DOMAIN_PROVIDER_END"]
DRAIN_DOMAIN = BOUNDS["domains"]["SIGNATURE_DOMAIN_PROVIDER_DRAIN"]
ROTATE_DOMAIN = BOUNDS["domains"]["SIGNATURE_DOMAIN_PROVIDER_ROTATE"]
EVIDENCE_DOMAIN = BOUNDS["domains"]["SIGNATURE_DOMAIN_EVIDENCE_ATTESTATION"]
AUDITOR_KEY = "key-auditor"

SAFETY_KEYS = (
    "safety_max_package_bytes",
    "safety_max_evidence_batch",
    "safety_max_expiry_steps_per_block",
    "safety_max_page_limit",
    "safety_max_tx_bytes",
    "safety_max_gas_per_tx",
    "safety_max_open_offers_per_provider",
)


def addr(byte: int) -> str:
    return f"{byte:02x}" * BOUNDS["account_address_bytes"]


SERVICE = addr(0x11)
BROADCASTER = addr(0x22)
PROVIDER_ACCOUNT = addr(0x33)
AUDITOR = addr(0x44)
AUTHORITY = addr(0x55)
OUTSIDER = addr(0x66)


def ok(idempotent: bool = False) -> dict[str, object]:
    return {"ok": True, "error": "", "idempotent": idempotent, "keep": True}


def err(code: str, keep: bool = False) -> dict[str, object]:
    return {"ok": False, "error": code, "idempotent": False, "keep": keep}


def apply(state: dict, step: dict) -> dict[str, object]:
    trial = copy.deepcopy(state)
    result = dispatch(trial, step)
    if result.get("keep"):
        state.clear()
        state.update(trial)
    return result


def dispatch(state: dict, step: dict) -> dict[str, object]:
    """Route one step. Harness operations are not transaction messages."""
    op = step.get("op")
    if not isinstance(op, str):
        return err("ERROR_CODE_MALFORMED")
    harness = HARNESS_OPS.get(op)
    if harness is not None:
        blocked = guard_decisions(state)
        if blocked is not None and op != "ApproveDecision":
            return blocked
        return harness(state, step)
    return dispatch_tx(state, step)


def dispatch_tx(state: dict, step: dict) -> dict[str, object]:
    """Transaction router. It cannot reach harness operations."""
    op = step.get("op")
    if not isinstance(op, str):
        return err("ERROR_CODE_MALFORMED")
    handler = TX_HANDLERS.get(op)
    if handler is None:
        return err("ERROR_CODE_MALFORMED")
    blocked = guard_decisions(state)
    if blocked is not None and op != "ApproveDecision":
        return blocked
    return handler(state, step)


def guard_decisions(state: dict) -> dict[str, object] | None:
    for row in state["decisions"].values():
        if row["approved"] or row["value_version"] != 0 or row["schema_version"] != 1:
            return err("ERROR_CODE_FORBIDDEN")
    if state["approved_council"]:
        return err("ERROR_CODE_FORBIDDEN")
    for flag in ("mint", "distribution", "slashing", "staking", "permissionless_validator_creation"):
        if state["modules"][flag]:
            return err("ERROR_CODE_FORBIDDEN")
    return None


def devnet_param(state: dict, decision_id: str) -> dict | None:
    if not state["allow_devnet_test_parameters"]:
        return None
    prefix = BOUNDS["devnet_chain_id_prefix"]
    if not str(state["chain_id"]).startswith(prefix):
        return None
    row = state["devnet_tests"].get(decision_id)
    if not isinstance(row, dict):
        return None
    if row.get("label") != BOUNDS["devnet_test_label"] or row.get("approved_policy"):
        return None
    return row


def unknown(step: dict, allowed: set[str]) -> dict[str, object] | None:
    extra = set(step) - allowed - {"op", "expect", "tx_hash", "note"}
    if extra:
        return err("ERROR_CODE_UNKNOWN_FIELD")
    return None


def require_fields(step: dict, names: tuple[str, ...]) -> dict[str, object] | None:
    for name in names:
        if name not in step:
            return err("ERROR_CODE_MALFORMED")
    return None


def as_int(value: object) -> int | None:
    if isinstance(value, bool) or not isinstance(value, int):
        return None
    return value


def check_id(value: object) -> str | None:
    if not isinstance(value, str) or ID_RE.fullmatch(value) is None:
        return "ERROR_CODE_BOUNDS"
    return None


def check_digest(value: object) -> str | None:
    if not isinstance(value, str) or len(value) != BOUNDS["digest_bytes"] * 2:
        return "ERROR_CODE_BOUNDS"
    if any(char not in "0123456789abcdef" for char in value):
        return "ERROR_CODE_BOUNDS"
    return None


def check_pubkey(value: object) -> str | None:
    width = BOUNDS["ed25519_public_bytes"] * 2
    if not isinstance(value, str) or len(value) != width:
        return "ERROR_CODE_BOUNDS"
    if any(char not in "0123456789abcdef" for char in value):
        return "ERROR_CODE_BOUNDS"
    return None


def pubkey_for(label: str) -> str:
    return hashlib.sha256(f"pubkey:{label}".encode("utf-8")).hexdigest()


ENDPOINT = hashlib.sha256(b"svn-endpoint-v1").hexdigest()
REASON = hashlib.sha256(b"svn-reason-v1").hexdigest()


def check_time(value: object) -> str | None:
    number = as_int(value)
    if number is None:
        return "ERROR_CODE_MALFORMED"
    if number < BOUNDS["unix_seconds_min"] or number > BOUNDS["unix_seconds_max"]:
        return "ERROR_CODE_BOUNDS"
    return None


def check_address(value: object) -> str | None:
    width = BOUNDS["account_address_bytes"] * 2
    if not isinstance(value, str) or len(value) != width:
        return "ERROR_CODE_BOUNDS"
    if any(char not in "0123456789abcdef" for char in value):
        return "ERROR_CODE_BOUNDS"
    return None


def effective_limit(state: dict, key: str) -> int:
    """The live governed value for one key, or that key's own ceiling."""
    ceiling = int(BOUNDS["governed_limit_caps"][key])
    current = state["governed_limits"].get(key)
    if isinstance(current, int) and not isinstance(current, bool) and 1 <= current <= ceiling:
        return current
    return ceiling


def check_package(value: object, state: dict) -> str | None:
    number = as_int(value)
    if number is None:
        return "ERROR_CODE_MALFORMED"
    if number < 1 or number > effective_limit(state, "max_package_bytes"):
        return "ERROR_CODE_BOUNDS"
    return None


def check_role(state: dict, signer: object, role: str) -> str | None:
    address_error = check_address(signer)
    if address_error:
        return address_error
    record = state["submitters"].get(signer)
    if record is None:
        return "ERROR_CODE_UNAUTHORIZED"
    if record["role"] != role:
        return "ERROR_CODE_FORBIDDEN"
    return None


def check_signature(sig: object, domain: str, key_id: str, bound_digest: str | None = None) -> str | None:
    if not isinstance(sig, dict):
        return "ERROR_CODE_MALFORMED"
    allowed = {"domain", "key_id", "valid", "role_name"}
    if bound_digest is not None:
        allowed.add("observation_digest")
    extra = set(sig) - allowed
    if extra:
        return "ERROR_CODE_UNKNOWN_FIELD"
    if sig.get("domain") != domain:
        return "ERROR_CODE_WRONG_DOMAIN"
    if sig.get("valid") is not True:
        return "ERROR_CODE_UNAUTHORIZED"
    if sig.get("key_id") != key_id:
        return "ERROR_CODE_UNAUTHORIZED"
    if bound_digest is not None and sig.get("observation_digest") != bound_digest:
        return "ERROR_CODE_INTEGRITY"
    return None


def business_gate(state: dict, step: dict, fields: tuple[str, ...]) -> dict[str, object] | None:
    business_id = step.get("business_id")
    id_error = check_id(business_id)
    if id_error:
        return err(id_error if business_id else "ERROR_CODE_MALFORMED")
    payload = {name: step[name] for name in fields if name in step}
    try:
        digest = hashlib.sha256(canonicalize(payload).encode("utf-8")).hexdigest()
    except ValueError:
        return err("ERROR_CODE_MALFORMED")
    previous = state["business"].get(business_id)
    if previous is None:
        state["_pending_business"] = (business_id, digest)
        return None
    if previous == digest:
        return ok(idempotent=True)
    return err("ERROR_CODE_REPLAY")


def commit_business(state: dict) -> None:
    pending = state.pop("_pending_business", None)
    if pending is not None:
        state["business"][pending[0]] = pending[1]


def pair_key(provider_id: str, content_id: str, version: int) -> str:
    return f"{provider_id}|{content_id}|{version}"


def reserve(provider: dict, amount: int) -> str | None:
    if amount > UINT64_MAX - provider["reserved_bytes"]:
        return "ERROR_CODE_OVERFLOW"
    total = provider["reserved_bytes"] + amount
    if total > provider["admitted_capacity_bytes"]:
        return "ERROR_CODE_CAPACITY"
    provider["reserved_bytes"] = total
    return None


def release(provider: dict, amount: int) -> str | None:
    if amount > provider["reserved_bytes"]:
        return "ERROR_CODE_UNDERFLOW"
    provider["reserved_bytes"] -= amount
    return None


def quorum_holds(validators: dict, down: list) -> bool:
    """More than two thirds of the projected voting power must be available."""
    total = 0
    available = 0
    for validator, power in sorted(validators.items()):
        if not isinstance(power, int) or isinstance(power, bool) or power <= 0:
            return False
        total += power
        if validator not in down:
            available += power
    if total <= 0 or total > SAFE_MAX:
        return False
    return available * 3 > total * 2


MIN_VALIDATOR_COUNT = 4


def power_set_invariant(validators: dict) -> str | None:
    """Hashed safety bound. It does not read liveness.

    Every activation point must keep at least MIN_VALIDATOR_COUNT validators
    and max_power * 3 < total_power. A set that fails is not scheduled.
    """
    if not validators:
        return "ERROR_CODE_FORBIDDEN"
    total = 0
    max_power = 0
    for power in validators.values():
        if not isinstance(power, int) or isinstance(power, bool) or power <= 0:
            return "ERROR_CODE_FORBIDDEN"
        total += power
        if power > max_power:
            max_power = power
    if len(validators) < MIN_VALIDATOR_COUNT or total > SAFE_MAX:
        return "ERROR_CODE_FORBIDDEN"
    if max_power * 3 >= total:
        return "ERROR_CODE_FORBIDDEN"
    return None


def liveness_would_halt(validators: dict, down: list) -> bool:
    """More than one third of power is offline.

    The assumption for a real network is that more than two thirds of voting
    power is online. One validator may be down. Losing more than one third
    means the environment produces no block. This predicate is not
    commit_possible and scheduling does not call it.
    """
    return not quorum_holds(validators, down)


def environment_withholds_block(state: dict) -> bool:
    """Environment-only. Not a consensus transition and not an app-hash input."""
    return liveness_would_halt(state["validators"], state["down"])


def commit_possible(state: dict) -> bool:
    return power_set_invariant(state["validators"]) is None


def intent_ok(state: dict, sig: object) -> str | None:
    if not isinstance(sig, dict):
        return "ERROR_CODE_MALFORMED"
    if sig.get("role_name") == "ACCOUNT_HOST_DELEGATE":
        return "ERROR_CODE_FORBIDDEN"
    signature_error = check_signature(sig, HOSTING, str(sig.get("key_id")))
    if signature_error == "ERROR_CODE_WRONG_DOMAIN":
        return signature_error
    if signature_error == "ERROR_CODE_UNKNOWN_FIELD":
        return signature_error
    param = devnet_param(state, "DEC-001")
    if param is None or param.get("role_name") != "DEVNET_TEST_ONLY":
        return "ERROR_CODE_DECISION_BLOCKED"
    if sig.get("role_name") != "DEVNET_TEST_ONLY":
        return "ERROR_CODE_FORBIDDEN"
    if signature_error:
        return signature_error
    return None


def sponsorship_seconds(state: dict, seconds: int) -> str | None:
    param = devnet_param(state, "DEC-005")
    if param is None:
        return "ERROR_CODE_DECISION_BLOCKED"
    expected = param.get("numeric_value")
    if not isinstance(expected, int) or isinstance(expected, bool) or expected <= 0:
        return "ERROR_CODE_DECISION_BLOCKED"
    if seconds != expected:
        return "ERROR_CODE_OUT_OF_RANGE"
    return None


def council_ready(state: dict) -> str | None:
    param = devnet_param(state, "DEC-008")
    if param is None:
        return "ERROR_CODE_DECISION_BLOCKED"
    required = param.get("numeric_value")
    delay = param.get("delay_seconds")
    if not isinstance(required, int) or isinstance(required, bool) or required <= 0:
        return "ERROR_CODE_DECISION_BLOCKED"
    if not isinstance(delay, int) or isinstance(delay, bool) or delay < 0:
        return "ERROR_CODE_DECISION_BLOCKED"
    if required > len(state["seats"]):
        return "ERROR_CODE_DECISION_BLOCKED"
    return None


def finish(state: dict, event: dict | None = None) -> dict[str, object]:
    commit_business(state)
    if event is not None:
        state["events"].append(event)
    return ok()


def op_register_provider(state: dict, step: dict) -> dict[str, object]:
    allowed = {
        "signer",
        "business_id",
        "provider_id",
        "signing_key_id",
        "signing_pubkey",
        "endpoint_manifest_digest",
        "declared_capacity_bytes",
        "operator_group_id",
    }
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    role_error = check_role(state, step["signer"], "ADMITTED_PROVIDER")
    if role_error:
        return err(role_error)
    for name in ("provider_id", "signing_key_id", "operator_group_id"):
        id_error = check_id(step[name])
        if id_error:
            return err(id_error)
    pubkey_error = check_pubkey(step["signing_pubkey"])
    if pubkey_error:
        return err(pubkey_error)
    digest_error = check_digest(step["endpoint_manifest_digest"])
    if digest_error:
        return err(digest_error)
    capacity = as_int(step["declared_capacity_bytes"])
    if capacity is None:
        return err("ERROR_CODE_MALFORMED")
    if capacity < 1 or capacity > UINT64_MAX:
        return err("ERROR_CODE_BOUNDS")
    gate = business_gate(state, step, tuple(sorted(allowed - {"signer"})))
    if gate:
        return gate
    if step["provider_id"] in state["providers"]:
        return err("ERROR_CODE_DUPLICATE_ID")
    state["providers"][step["provider_id"]] = {
        "account": step["signer"],
        "signing_key_id": step["signing_key_id"],
        "signing_pubkey": step["signing_pubkey"],
        "endpoint_manifest_digest": step["endpoint_manifest_digest"],
        "key_generation": 1,
        "state": "APPLIED",
        "admitted_capacity_bytes": capacity,
        "reserved_bytes": 0,
        "operator_group_id": step["operator_group_id"],
        "previous_keys": [],
        "previous_pubkeys": [],
    }
    return finish(state, {"type": "ProviderRegistered", "provider_id": step["provider_id"], "state": "APPLIED"})


def op_admit_provider(state: dict, step: dict) -> dict[str, object]:
    return err("ERROR_CODE_FORBIDDEN")


def op_register_content(state: dict, step: dict) -> dict[str, object]:
    allowed = {
        "signer",
        "business_id",
        "content_id",
        "content_version",
        "package_bytes",
        "manifest_digest",
        "hosting_intent",
    }
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    role_error = check_role(state, step["signer"], "ADMITTED_SERVICE")
    if role_error:
        return err(role_error)
    id_error = check_id(step["content_id"])
    if id_error:
        return err(id_error)
    version = as_int(step["content_version"])
    if version is None:
        return err("ERROR_CODE_MALFORMED")
    if version != 1:
        return err("ERROR_CODE_OUT_OF_RANGE")
    package_error = check_package(step["package_bytes"], state)
    if package_error:
        return err(package_error)
    digest_error = check_digest(step["manifest_digest"])
    if digest_error:
        return err(digest_error)
    intent_error = intent_ok(state, step["hosting_intent"])
    if intent_error:
        return err(intent_error)
    gate = business_gate(
        state,
        step,
        ("content_id", "content_version", "hosting_intent", "manifest_digest", "package_bytes"),
    )
    if gate:
        return gate
    if step["content_id"] in state["contents"]:
        return err("ERROR_CODE_DUPLICATE_ID")
    state["contents"][step["content_id"]] = {
        "content_version": version,
        "manifest_digest": step["manifest_digest"],
        "package_bytes": step["package_bytes"],
        "submitting_host": step["signer"],
        "state": "PENDING",
        "tombstone": None,
    }
    state["publication"][step["content_id"]] = "PUBLICATION_STATUS_NOT_REQUESTED"
    return finish(
        state,
        {
            "type": "ContentRegistered",
            "content_id": step["content_id"],
            "package_bytes": str(step["package_bytes"]),
        },
    )


def withdraw_content(state: dict, signer: str, content_id: str, version: object, reason_digest: str, authority: bool) -> dict[str, object]:
    content = state["contents"].get(content_id)
    if content is None:
        return err("ERROR_CODE_NOT_FOUND")
    if not authority and content["submitting_host"] != signer:
        return err("ERROR_CODE_FORBIDDEN")
    if content["state"] == "WITHDRAWN":
        return err("ERROR_CODE_WITHDRAWN")
    number = as_int(version)
    if number is None:
        return err("ERROR_CODE_MALFORMED")
    if number != content["content_version"]:
        return err("ERROR_CODE_STALE_VERSION")
    content["state"] = "WITHDRAWN"
    content["removal_promise_approved"] = False
    content["tombstone"] = {
        "history_erased": False,
        "retention_decision_id": "DEC-004",
        "retention_value_version": 0,
        "committed_unix_seconds": state["chain_time"],
        "reason_digest": reason_digest,
    }
    for offer_id in [offer_id for offer_id, offer in state["offers"].items() if offer["content_id"] == content_id]:
        del state["offers"][offer_id]
    return finish(state, {"type": "ContentWithdrawn", "content_id": content_id, "history_erased": False})


def op_withdraw_content(state: dict, step: dict) -> dict[str, object]:
    allowed = {"signer", "business_id", "content_id", "content_version", "hosting_intent", "reason_digest"}
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    role_error = check_role(state, step["signer"], "ADMITTED_SERVICE")
    if role_error:
        return err(role_error)
    digest_error = check_digest(step["reason_digest"])
    if digest_error:
        return err(digest_error)
    intent_error = intent_ok(state, step["hosting_intent"])
    if intent_error:
        return err(intent_error)
    gate = business_gate(state, step, ("content_id", "content_version", "hosting_intent", "reason_digest"))
    if gate:
        return gate
    return withdraw_content(state, step["signer"], step["content_id"], step["content_version"], step["reason_digest"], authority=False)


def op_offer(state: dict, step: dict) -> dict[str, object]:
    allowed = {
        "signer",
        "business_id",
        "offer_id",
        "content_id",
        "content_version",
        "provider_id",
        "package_bytes",
        "manifest_digest",
        "terms_digest",
        "start_unix_seconds",
        "deadline_unix_seconds",
        "expiry_unix_seconds",
        "term_version",
    }
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    role_error = check_role(state, step["signer"], "ADMITTED_SERVICE")
    if role_error:
        return err(role_error)
    for name in ("offer_id", "content_id", "provider_id"):
        id_error = check_id(step[name])
        if id_error:
            return err(id_error)
    for name in ("manifest_digest", "terms_digest"):
        digest_error = check_digest(step[name])
        if digest_error:
            return err(digest_error)
    for name in ("start_unix_seconds", "deadline_unix_seconds", "expiry_unix_seconds"):
        time_error = check_time(step[name])
        if time_error:
            return err(time_error)
    version = as_int(step["content_version"])
    term_version = as_int(step["term_version"])
    if version is None or term_version is None:
        return err("ERROR_CODE_MALFORMED")
    package_error = check_package(step["package_bytes"], state)
    if package_error:
        return err(package_error)
    gate = business_gate(state, step, tuple(sorted(allowed - {"signer"})))
    if gate:
        return gate
    if step["offer_id"] in state["offers"]:
        return err("ERROR_CODE_DUPLICATE_ID")
    content = state["contents"].get(step["content_id"])
    if content is None:
        return err("ERROR_CODE_NOT_FOUND")
    if content["state"] == "WITHDRAWN":
        return err("ERROR_CODE_WITHDRAWN")
    if content["state"] != "PENDING" or content["content_version"] != version:
        return err("ERROR_CODE_CONFLICT")
    if content["manifest_digest"] != step["manifest_digest"] or content["package_bytes"] != step["package_bytes"]:
        return err("ERROR_CODE_INTEGRITY")
    provider = state["providers"].get(step["provider_id"])
    if provider is None:
        return err("ERROR_CODE_NOT_FOUND")
    if provider["state"] != "ADMITTED":
        return err("ERROR_CODE_FORBIDDEN")
    if step["expiry_unix_seconds"] <= state["chain_time"]:
        return err("ERROR_CODE_EXPIRED")
    if step["deadline_unix_seconds"] <= step["start_unix_seconds"]:
        return err("ERROR_CODE_OUT_OF_RANGE")
    term_error = sponsorship_seconds(state, step["deadline_unix_seconds"] - step["start_unix_seconds"])
    if term_error:
        return err(term_error)
    if term_version != 1:
        return err("ERROR_CODE_STALE_VERSION")
    if pair_key(step["provider_id"], step["content_id"], version) in state["pair_index"]:
        return err("ERROR_CODE_DUPLICATE_ID")
    open_offers = sum(1 for offer in state["offers"].values() if offer["provider_id"] == step["provider_id"])
    if open_offers >= effective_limit(state, "max_open_offers"):
        return err("ERROR_CODE_BOUNDS")
    projected = reserve_probe(provider, step["package_bytes"])
    if projected:
        return err(projected)
    state["offers"][step["offer_id"]] = {
        "offer_id": step["offer_id"],
        "content_id": step["content_id"],
        "content_version": version,
        "provider_id": step["provider_id"],
        "package_bytes": step["package_bytes"],
        "manifest_digest": step["manifest_digest"],
        "terms_digest": step["terms_digest"],
        "start_unix_seconds": step["start_unix_seconds"],
        "deadline_unix_seconds": step["deadline_unix_seconds"],
        "expiry_unix_seconds": step["expiry_unix_seconds"],
        "term_version": term_version,
        "scheduler": step["signer"],
    }
    return finish(state, {"type": "LeaseOffered", "offer_id": step["offer_id"]})


def reserve_probe(provider: dict, amount: int) -> str | None:
    if amount > UINT64_MAX - provider["reserved_bytes"]:
        return "ERROR_CODE_OVERFLOW"
    if provider["reserved_bytes"] + amount > provider["admitted_capacity_bytes"]:
        return "ERROR_CODE_CAPACITY"
    return None


def op_accept(state: dict, step: dict) -> dict[str, object]:
    allowed = {"signer", "business_id", "offer_id", "lease_id", "provider_acceptance"}
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    signer_error = check_address(step["signer"])
    if signer_error:
        return err(signer_error)
    record = state["submitters"].get(step["signer"])
    if record is None:
        return err("ERROR_CODE_UNAUTHORIZED")
    if record["role"] not in {"BROADCASTER", "ADMITTED_PROVIDER"}:
        return err("ERROR_CODE_FORBIDDEN")
    for name in ("offer_id", "lease_id"):
        id_error = check_id(step[name])
        if id_error:
            return err(id_error)
    acceptance = step["provider_acceptance"]
    if not isinstance(acceptance, dict):
        return err("ERROR_CODE_MALFORMED")
    if set(acceptance) - {"domain", "key_id", "valid", "role_name"}:
        return err("ERROR_CODE_UNKNOWN_FIELD")
    if acceptance.get("domain") != ACCEPT_DOMAIN:
        return err("ERROR_CODE_WRONG_DOMAIN")
    if acceptance.get("valid") is not True:
        return err("ERROR_CODE_UNAUTHORIZED")
    gate = business_gate(state, step, ("lease_id", "offer_id", "provider_acceptance"))
    if gate:
        return gate
    offer = state["offers"].get(step["offer_id"])
    if offer is None:
        return err("ERROR_CODE_NOT_FOUND")
    provider = state["providers"][offer["provider_id"]]
    if acceptance.get("key_id") != provider["signing_key_id"]:
        return err("ERROR_CODE_UNAUTHORIZED")
    if record["role"] == "ADMITTED_PROVIDER" and step["signer"] != provider["account"]:
        return err("ERROR_CODE_FORBIDDEN")
    if step["lease_id"] in state["leases"]:
        return err("ERROR_CODE_DUPLICATE_ID")
    if state["chain_time"] >= offer["expiry_unix_seconds"]:
        return err("ERROR_CODE_EXPIRED")
    content = state["contents"][offer["content_id"]]
    if content["state"] == "WITHDRAWN":
        return err("ERROR_CODE_WITHDRAWN")
    if provider["state"] != "ADMITTED":
        return err("ERROR_CODE_FORBIDDEN")
    key = pair_key(offer["provider_id"], offer["content_id"], offer["content_version"])
    if key in state["pair_index"]:
        return err("ERROR_CODE_DUPLICATE_ID")
    reserve_error = reserve(provider, offer["package_bytes"])
    if reserve_error:
        return err(reserve_error)
    state["leases"][step["lease_id"]] = {
        "lease_id": step["lease_id"],
        "offer_id": offer["offer_id"],
        "content_id": offer["content_id"],
        "content_version": offer["content_version"],
        "provider_id": offer["provider_id"],
        "package_bytes": offer["package_bytes"],
        "manifest_digest": offer["manifest_digest"],
        "terms_digest": offer["terms_digest"],
        "start_unix_seconds": offer["start_unix_seconds"],
        "deadline_unix_seconds": offer["deadline_unix_seconds"],
        "state": "ACTIVE",
        "term_version": offer["term_version"],
        "policy_version": state["policy_version"],
        "reservation_held": True,
    }
    state["pair_index"][key] = step["lease_id"]
    del state["offers"][offer["offer_id"]]
    return finish(
        state,
        {
            "type": "LeaseAccepted",
            "lease_id": step["lease_id"],
            "package_bytes": str(offer["package_bytes"]),
        },
    )


def op_renew(state: dict, step: dict) -> dict[str, object]:
    allowed = {
        "signer",
        "business_id",
        "lease_id",
        "term_version",
        "deadline_unix_seconds",
        "package_bytes",
        "manifest_digest",
        "provider_signature",
        "evidence_id",
    }
    bad = unknown(step, allowed) or require_fields(
        step,
        (
            "signer",
            "business_id",
            "lease_id",
            "term_version",
            "deadline_unix_seconds",
            "package_bytes",
            "manifest_digest",
            "provider_signature",
        ),
    )
    if bad:
        return bad
    lease = state["leases"].get(step["lease_id"])
    if lease is None:
        return err("ERROR_CODE_NOT_FOUND")
    provider = state["providers"][lease["provider_id"]]
    signature_error = check_signature(step["provider_signature"], RENEW_DOMAIN, provider["signing_key_id"])
    if signature_error:
        return err(signature_error)
    role_error = check_role(state, step["signer"], "BROADCASTER")
    provider_role = check_role(state, step["signer"], "ADMITTED_PROVIDER")
    if role_error and provider_role:
        return err(role_error or provider_role)
    if provider_role is None and step["signer"] != provider["account"]:
        return err("ERROR_CODE_FORBIDDEN")
    digest_error = check_digest(step["manifest_digest"])
    if digest_error:
        return err(digest_error)
    gate = business_gate(
        state,
        step,
        (
            "deadline_unix_seconds",
            "evidence_id",
            "lease_id",
            "manifest_digest",
            "package_bytes",
            "provider_signature",
            "term_version",
        ),
    )
    if gate:
        return gate
    if lease["state"] not in {"ACTIVE", "RENEWED"} or not lease["reservation_held"]:
        return err("ERROR_CODE_CONFLICT")
    content = state["contents"][lease["content_id"]]
    if content["state"] == "WITHDRAWN":
        return err("ERROR_CODE_WITHDRAWN")
    term_version = as_int(step["term_version"])
    if term_version is None:
        return err("ERROR_CODE_MALFORMED")
    if term_version != lease["term_version"] + 1:
        return err("ERROR_CODE_STALE_VERSION")
    if step["package_bytes"] != lease["package_bytes"] or step["manifest_digest"] != lease["manifest_digest"]:
        return err("ERROR_CODE_CONFLICT")
    time_error = check_time(step["deadline_unix_seconds"])
    if time_error:
        return err(time_error)
    extension = step["deadline_unix_seconds"] - lease["deadline_unix_seconds"]
    term_error = sponsorship_seconds(state, extension)
    if term_error:
        return err(term_error)
    if step.get("evidence_id"):
        evidence = state["evidence"].get(step["evidence_id"])
        if evidence is None:
            return err("ERROR_CODE_NOT_FOUND")
        if evidence["state"] == "EXPIRED" or evidence["expiry_unix_seconds"] <= state["chain_time"]:
            return err("ERROR_CODE_EXPIRED")
        return err("ERROR_CODE_FORBIDDEN")
    before = provider["reserved_bytes"]
    lease["term_version"] = term_version
    lease["deadline_unix_seconds"] = step["deadline_unix_seconds"]
    lease["state"] = "RENEWED"
    if provider["reserved_bytes"] != before:
        return err("ERROR_CODE_CONFLICT")
    return finish(state, {"type": "LeaseRenewed", "lease_id": step["lease_id"], "term_version": term_version})


def op_end(state: dict, step: dict) -> dict[str, object]:
    allowed = {"signer", "business_id", "lease_id", "provider_signature", "authority_end"}
    bad = unknown(step, allowed) or require_fields(step, ("signer", "business_id", "lease_id", "authority_end"))
    if bad:
        return bad
    if not isinstance(step["authority_end"], bool):
        return err("ERROR_CODE_MALFORMED")
    lease = state["leases"].get(step["lease_id"])
    if lease is None:
        return err("ERROR_CODE_NOT_FOUND")
    provider = state["providers"][lease["provider_id"]]
    if step["authority_end"]:
        role_error = check_role(state, step["signer"], "AUTHORITY")
        if role_error:
            return err(role_error)
        if "provider_signature" in step:
            return err("ERROR_CODE_UNKNOWN_FIELD")
    else:
        if "provider_signature" not in step:
            return err("ERROR_CODE_MALFORMED")
        signature_error = check_signature(step["provider_signature"], END_DOMAIN, provider["signing_key_id"])
        if signature_error:
            return err(signature_error)
        broadcaster = check_role(state, step["signer"], "BROADCASTER")
        provider_role = check_role(state, step["signer"], "ADMITTED_PROVIDER")
        if broadcaster and provider_role:
            return err("ERROR_CODE_FORBIDDEN")
        if provider_role is None and step["signer"] != provider["account"]:
            return err("ERROR_CODE_FORBIDDEN")
    fields = ("authority_end", "lease_id")
    if not step["authority_end"]:
        fields = ("authority_end", "lease_id", "provider_signature")
    gate = business_gate(state, step, fields)
    if gate:
        return gate
    if not lease["reservation_held"]:
        return err("ERROR_CODE_UNDERFLOW")
    release_error = release(provider, lease["package_bytes"])
    if release_error:
        return err(release_error)
    lease["reservation_held"] = False
    lease["state"] = "ENDED"
    key = pair_key(lease["provider_id"], lease["content_id"], lease["content_version"])
    state["pair_index"].pop(key, None)
    return finish(
        state,
        {"type": "LeaseEnded", "lease_id": step["lease_id"], "released_bytes": str(lease["package_bytes"])},
    )


def op_audit(state: dict, step: dict) -> dict[str, object]:
    allowed = {
        "signer",
        "business_id",
        "batch_id",
        "epoch",
        "evidence_id",
        "kind",
        "lease_id",
        "content_id",
        "content_version",
        "expiry_unix_seconds",
        "attestation",
        "auditor_signature",
        "observation_digest",
        "item_count",
    }
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed - {"item_count"})))
    if bad:
        return bad
    role_error = check_role(state, step["signer"], "AUDITOR")
    if role_error:
        return err(role_error)
    digest_error = check_digest(step["observation_digest"])
    if digest_error:
        return err(digest_error)
    signature_error = check_signature(
        step["auditor_signature"],
        EVIDENCE_DOMAIN,
        AUDITOR_KEY,
        bound_digest=step["observation_digest"],
    )
    if signature_error:
        return err(signature_error)
    for name in ("batch_id", "evidence_id", "lease_id", "content_id"):
        id_error = check_id(step[name])
        if id_error:
            return err(id_error)
    if step["kind"] not in {"PROVIDER_RECEIPT", "AUDITOR_ATTESTATION"}:
        return err("ERROR_CODE_MALFORMED")
    if "item_count" in step:
        item_count = as_int(step["item_count"])
        if item_count is None:
            return err("ERROR_CODE_MALFORMED")
    else:
        item_count = 1
    if item_count < 1 or item_count > effective_limit(state, "max_evidence_batch"):
        return err("ERROR_CODE_BOUNDS")
    if step["attestation"] is not True:
        return err("ERROR_CODE_MALFORMED")
    epoch = as_int(step["epoch"])
    if epoch is None or epoch != state["epoch"]:
        return err("ERROR_CODE_STALE_VERSION" if epoch is not None else "ERROR_CODE_MALFORMED")
    time_error = check_time(step["expiry_unix_seconds"])
    if time_error:
        return err(time_error)
    gate = business_gate(state, step, tuple(sorted(allowed - {"signer"})))
    if gate:
        return gate
    if step["evidence_id"] in state["evidence"] or step["batch_id"] in state["batches"]:
        return err("ERROR_CODE_DUPLICATE_ID")
    if step["expiry_unix_seconds"] <= state["chain_time"]:
        return err("ERROR_CODE_EXPIRED")
    if step["lease_id"] not in state["leases"]:
        return err("ERROR_CODE_NOT_FOUND")
    lease = state["leases"][step["lease_id"]]
    deadline_before = lease["deadline_unix_seconds"]
    state_before = lease["state"]
    recorded = "RECORDED"
    if step["observation_digest"] != lease["manifest_digest"]:
        recorded = "REJECTED"
    state["evidence"][step["evidence_id"]] = {
        "batch_id": step["batch_id"],
        "kind": step["kind"],
        "state": recorded,
        "lease_id": step["lease_id"],
        "content_id": step["content_id"],
        "expiry_unix_seconds": step["expiry_unix_seconds"],
        "observation_digest": step["observation_digest"],
        "attestation": True,
    }
    state["batches"].add(step["batch_id"])
    if lease["deadline_unix_seconds"] != deadline_before or lease["state"] != state_before:
        return err("ERROR_CODE_CONFLICT")
    return finish(
        state,
        {"type": "EvidenceAccepted", "evidence_id": step["evidence_id"], "attestation": True, "kind": step["kind"]},
    )


DECIMAL_RE = re.compile(r"0|[1-9][0-9]*")
INT64_MAX = 9223372036854775807
PAYLOAD_FIELDS = {
    "validatorPower": "validator_power",
    "governedLimit": "governed_limit",
    "admitProvider": "admit_provider",
    "disableProvider": "disable_provider",
    "forceWithdraw": "force_withdraw",
}


def payload_digest(payload: dict) -> str:
    return hashlib.sha256(canonicalize(payload).encode("utf-8")).hexdigest()


def decimal_uint(value: object) -> int | None:
    if not isinstance(value, str) or DECIMAL_RE.fullmatch(value) is None:
        return None
    number = int(value)
    if number > UINT64_MAX:
        return None
    return number


def standard_b64(raw: bytes) -> str:
    return base64.b64encode(raw).decode("ascii")


def b64_fixed(value: object, size: int) -> bytes | None:
    if not isinstance(value, str):
        return None
    try:
        raw = base64.b64decode(value, validate=True)
    except (binascii.Error, ValueError):
        return None
    if len(raw) != size:
        return None
    return raw


def digest_message(value: object) -> str | None:
    if not isinstance(value, dict) or set(value) != {"algorithm", "value"}:
        return None
    if value["algorithm"] != "DIGEST_ALGORITHM_SHA256":
        return None
    raw = b64_fixed(value["value"], BOUNDS["digest_bytes"])
    if raw is None or standard_b64(raw) != value["value"]:
        return None
    return raw.hex()


def digest_json(hex_text: str) -> dict[str, str]:
    return {"algorithm": "DIGEST_ALGORITHM_SHA256", "value": standard_b64(bytes.fromhex(hex_text))}


def parse_activation(value: object) -> dict | None:
    if not isinstance(value, dict) or len(value) != 1:
        return None
    if set(value) == {"unixSeconds"}:
        number = decimal_uint(value["unixSeconds"])
        if number is None or number > SAFE_MAX or check_time(number):
            return None
        return {"kind": "time", "unix_seconds": number}
    if set(value) == {"height"}:
        number = decimal_uint(value["height"])
        if number is None or number < 1 or number > SAFE_MAX:
            return None
        return {"kind": "height", "height": number}
    return None


def activation_json(activation: dict) -> dict[str, str]:
    if activation["kind"] == "time":
        return {"unixSeconds": str(activation["unix_seconds"])}
    return {"height": str(activation["height"])}


def activation_is_future(activation: dict, state: dict) -> bool:
    if activation["kind"] == "time":
        return activation["unix_seconds"] > state["chain_time"]
    return activation["height"] > state["height"]


def activation_is_due(activation: dict, state: dict) -> bool:
    if activation["kind"] == "time":
        return state["chain_time"] >= activation["unix_seconds"]
    return state["height"] >= activation["height"]


def activation_identity(change: dict) -> tuple:
    """The due point of one power change. Height and time are not one order.

    Comparing a height with a unix time would choose a block-time policy.
    DEC-012 keeps the kinds separate: a mix is ERROR_CODE_CONFLICT.
    """
    activation = change["activation"]
    if activation["kind"] == "height":
        return ("height", activation["height"])
    return ("time", activation["unix_seconds"])


def activation_sort_key(item: dict) -> tuple:
    return (activation_identity(item), item["proposal_id"])


def parse_proposal_payload(payload: object) -> tuple[dict, dict] | str:
    """Parse proto3 JSON for ProposalPayload.

    Returns the normalized record and the rebuilt proto3 JSON object.
    The payload digest is SHA-256 of SVN-JCS-1 of that rebuilt object.
    """
    if not isinstance(payload, dict) or len(payload) != 1:
        return "ERROR_CODE_MALFORMED"
    field = next(iter(payload))
    kind = PAYLOAD_FIELDS.get(field)
    body = payload[field]
    if kind is None or not isinstance(body, dict):
        return "ERROR_CODE_MALFORMED"
    if kind == "validator_power":
        # Proto3 JSON omits the default. power 0 is that default, so a removal
        # has no power field. A present "0" is not the canonical preimage.
        required = {"consensusPubkey", "nodeId", "reasonDigest", "activation"}
        if set(body) == required:
            power = 0
        elif set(body) == required | {"power"}:
            power = decimal_uint(body["power"])
            if power == 0:
                return "ERROR_CODE_MALFORMED"
        else:
            return "ERROR_CODE_MALFORMED"
        raw_key = b64_fixed(body["consensusPubkey"], BOUNDS["consensus_pubkey_bytes"])
        reason = digest_message(body["reasonDigest"])
        activation = parse_activation(body["activation"])
        if raw_key is None or standard_b64(raw_key) != body["consensusPubkey"]:
            return "ERROR_CODE_BOUNDS"
        if power is None or power > INT64_MAX or reason is None or activation is None or check_id(body["nodeId"]):
            return "ERROR_CODE_MALFORMED" if power is None or activation is None or reason is None else "ERROR_CODE_BOUNDS"
        normalized = {
            "kind": kind,
            "consensus_pubkey": raw_key.hex(),
            "power": power,
            "node_id": body["nodeId"],
            "reason_digest": reason,
            "activation": activation,
        }
        rebuilt_body = {
            "activation": activation_json(activation),
            "consensusPubkey": standard_b64(raw_key),
            "nodeId": body["nodeId"],
            "reasonDigest": digest_json(reason),
        }
        if power != 0:
            rebuilt_body["power"] = str(power)
        rebuilt = {"validatorPower": rebuilt_body}
        return normalized, rebuilt
    if kind == "governed_limit":
        if set(body) != {"limitKey", "value"}:
            return "ERROR_CODE_MALFORMED"
        value = decimal_uint(body["value"])
        if check_id(body["limitKey"]) or value is None or value < 1:
            return "ERROR_CODE_BOUNDS" if value is not None else "ERROR_CODE_MALFORMED"
        normalized = {"kind": kind, "limit_key": body["limitKey"], "value": value}
        rebuilt = {"governedLimit": {"limitKey": body["limitKey"], "value": str(value)}}
        return normalized, rebuilt
    if kind in {"admit_provider", "disable_provider"}:
        if set(body) != {"providerId"} or check_id(body["providerId"]):
            return "ERROR_CODE_BOUNDS" if isinstance(body.get("providerId"), str) else "ERROR_CODE_MALFORMED"
        json_name = "admitProvider" if kind == "admit_provider" else "disableProvider"
        normalized = {"kind": kind, "provider_id": body["providerId"]}
        rebuilt = {json_name: {"providerId": body["providerId"]}}
        return normalized, rebuilt
    if set(body) != {"contentId", "contentVersion", "reasonDigest"}:
        return "ERROR_CODE_MALFORMED"
    version = decimal_uint(body["contentVersion"])
    reason = digest_message(body["reasonDigest"])
    if check_id(body["contentId"]) or version is None or reason is None:
        return "ERROR_CODE_MALFORMED" if version is None or reason is None else "ERROR_CODE_BOUNDS"
    normalized = {
        "kind": kind,
        "content_id": body["contentId"],
        "content_version": version,
        "reason_digest": reason,
    }
    rebuilt = {
        "forceWithdraw": {
            "contentId": body["contentId"],
            "contentVersion": str(version),
            "reasonDigest": digest_json(reason),
        }
    }
    return normalized, rebuilt


def validated_payload(state: dict, step: dict) -> dict[str, object] | dict:
    parsed = parse_proposal_payload(step.get("payload"))
    if isinstance(parsed, str):
        return err(parsed)
    normalized, rebuilt = parsed
    try:
        if canonicalize(step["payload"]) != canonicalize(rebuilt):
            return err("ERROR_CODE_MALFORMED")
        digest = payload_digest(rebuilt)
    except ValueError:
        return err("ERROR_CODE_MALFORMED")
    if digest != step.get("payload_digest"):
        return err("ERROR_CODE_INTEGRITY")
    parameter_key = step.get("parameter_key")
    kind = normalized["kind"]
    if kind == "validator_power":
        if parameter_key != f"validator-{normalized['node_id']}":
            return err("ERROR_CODE_MALFORMED")
        if not activation_is_future(normalized["activation"], state):
            return err("ERROR_CODE_OUT_OF_RANGE")
    elif kind == "governed_limit":
        if parameter_key != normalized["limit_key"]:
            return err("ERROR_CODE_MALFORMED")
    elif kind in {"admit_provider", "disable_provider"}:
        prefix = "admit" if kind == "admit_provider" else "disable"
        if parameter_key != f"{prefix}-{normalized['provider_id']}":
            return err("ERROR_CODE_MALFORMED")
    elif parameter_key != f"withdraw-{normalized['content_id']}":
        return err("ERROR_CODE_MALFORMED")
    return normalized


def keep_error(state: dict, code: str) -> dict[str, object]:
    state.pop("_pending_business", None)
    return err(code, keep=True)


def _apply_projected_change(validators: dict, pubkeys: dict, change: dict) -> str | None:
    node_id = change["node_id"]
    power = change["power"]
    pubkey = change["consensus_pubkey"]
    if power > 0:
        for other_id, other_pub in pubkeys.items():
            if other_pub == pubkey and other_id != node_id:
                return "ERROR_CODE_DUPLICATE_ID"
        validators[node_id] = power
        pubkeys[node_id] = pubkey
        return None
    if node_id not in validators:
        return "ERROR_CODE_NOT_FOUND"
    if pubkeys.get(node_id) != pubkey:
        return "ERROR_CODE_CONFLICT"
    del validators[node_id]
    pubkeys.pop(node_id, None)
    return None


def _checkpoint_projected_set(validators: dict) -> str | None:
    """Hashed power bound after one activation point. Does not read liveness."""
    return power_set_invariant(validators)


def project_power_changes(state: dict, extra: dict | None = None) -> str | None:
    """Simulate pending power changes in due order.

    The check runs after each activation point, not only after the last one.
    A later add does not excuse an earlier point that would leave no commit.
    """
    validators = dict(state["validators"])
    pubkeys = dict(state["validator_pubkeys"])
    changes = [
        item
        for item in state["pending_power"]
        if extra is None or item.get("proposal_id") != extra.get("proposal_id")
    ]
    if extra is not None:
        changes.append(extra)
    kinds = {item["activation"]["kind"] for item in changes}
    if len(kinds) > 1:
        return "ERROR_CODE_CONFLICT"
    changes.sort(key=activation_sort_key)
    index = 0
    while index < len(changes):
        point = activation_identity(changes[index])
        group = []
        while index < len(changes) and activation_identity(changes[index]) == point:
            group.append(changes[index])
            index += 1
        node_ids = [item["node_id"] for item in group]
        if len(node_ids) != len(set(node_ids)):
            return "ERROR_CODE_CONFLICT"
        # Bind a removal to the key the validator holds now. A payload that
        # names some other key must not free the real key for a later add,
        # and proposal id must not choose which error comes back.
        bound_keys: list[str] = []
        for item in group:
            if item["power"] == 0:
                current = pubkeys.get(item["node_id"])
                if item["node_id"] not in validators or current != item["consensus_pubkey"]:
                    return "ERROR_CODE_CONFLICT"
            # Payload key, not the current key. Rewriting a bad removal onto
            # the current key would hide this comparison inside the duplicate check.
            bound_keys.append(item["consensus_pubkey"])
        if len(bound_keys) != len(set(bound_keys)):
            return "ERROR_CODE_CONFLICT"
        for item in group:
            problem = _apply_projected_change(validators, pubkeys, item)
            if problem:
                return problem
        problem = _checkpoint_projected_set(validators)
        if problem:
            return problem
    return None


def commit_validator_power(state: dict, change: dict) -> str | None:
    node_id = change["node_id"]
    if change["power"] == 0:
        if node_id not in state["validators"]:
            return "ERROR_CODE_NOT_FOUND"
        if state["validator_pubkeys"].get(node_id) != change["consensus_pubkey"]:
            return "ERROR_CODE_CONFLICT"
        state["validators"].pop(node_id, None)
        state["validator_pubkeys"].pop(node_id, None)
        state["validator_reasons"].pop(node_id, None)
        return None
    state["validators"][node_id] = change["power"]
    state["validator_pubkeys"][node_id] = change["consensus_pubkey"]
    state["validator_reasons"][node_id] = change["reason_digest"]
    return None


def apply_due_validator_power(state: dict) -> str | None:
    due = [item for item in state["pending_power"] if activation_is_due(item["activation"], state)]
    waiting = [item for item in state["pending_power"] if item not in due]
    due.sort(key=activation_sort_key)
    state["pending_power"] = waiting
    for item in due:
        problem = commit_validator_power(state, item)
        if problem:
            return problem
    return None


def apply_proposal_payload(state: dict, proposal: dict) -> str | None:
    payload = proposal["payload"]
    kind = payload["kind"]
    if kind == "governed_limit":
        ceiling = BOUNDS["governed_limit_caps"].get(payload["limit_key"])
        if ceiling is None or payload["value"] > int(ceiling):
            return "ERROR_CODE_BOUNDS"
        state["governed_limits"][payload["limit_key"]] = payload["value"]
        return None
    if kind == "validator_power":
        if not activation_is_future(payload["activation"], state):
            return "ERROR_CODE_OUT_OF_RANGE"
        scheduled = {
            "proposal_id": proposal["proposal_id"],
            "node_id": payload["node_id"],
            "consensus_pubkey": payload["consensus_pubkey"],
            "power": payload["power"],
            "reason_digest": payload["reason_digest"],
            "activation": payload["activation"],
        }
        projected = project_power_changes(state, scheduled)
        if projected:
            return projected
        state["pending_power"].append(scheduled)
        return None
    if kind == "admit_provider":
        provider = state["providers"].get(payload["provider_id"])
        if provider is None:
            return "ERROR_CODE_NOT_FOUND"
        param = devnet_param(state, "DEC-002")
        if param is None or param.get("role_name") != "DEVNET_TEST_ONLY":
            return "ERROR_CODE_DECISION_BLOCKED"
        if provider["operator_group_id"] != "devnet-test-only" or provider["state"] != "APPLIED":
            return "ERROR_CODE_FORBIDDEN"
        provider["state"] = "ADMITTED"
        return None
    if kind == "disable_provider":
        provider = state["providers"].get(payload["provider_id"])
        if provider is None:
            return "ERROR_CODE_NOT_FOUND"
        if provider["state"] not in {"ADMITTED", "DRAINING"}:
            return "ERROR_CODE_CONFLICT"
        provider["state"] = "DISABLED"
        return None
    content = state["contents"].get(payload["content_id"])
    if content is None:
        return "ERROR_CODE_NOT_FOUND"
    param = devnet_param(state, "DEC-003")
    if param is None or param.get("role_name") != "DEVNET_TEST_ONLY":
        return "ERROR_CODE_DECISION_BLOCKED"
    if content["state"] == "WITHDRAWN":
        return "ERROR_CODE_WITHDRAWN"
    if payload["content_version"] != content["content_version"]:
        return "ERROR_CODE_STALE_VERSION"
    content["state"] = "WITHDRAWN"
    content["removal_promise_approved"] = False
    content["tombstone"] = {
        "history_erased": False,
        "retention_decision_id": "DEC-004",
        "retention_value_version": 0,
        "committed_unix_seconds": state["chain_time"],
        "reason_digest": payload["reason_digest"],
    }
    for offer_id in [offer_id for offer_id, offer in state["offers"].items() if offer["content_id"] == payload["content_id"]]:
        del state["offers"][offer_id]
    return None


def op_propose(state: dict, step: dict) -> dict[str, object]:
    allowed = {"signer", "business_id", "proposal_id", "parameter_key", "base_policy_version", "payload", "payload_digest", "expiry_unix_seconds"}
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    ready = council_ready(state)
    if ready:
        return err(ready)
    if step["signer"] not in state["seats"]:
        return err("ERROR_CODE_UNAUTHORIZED")
    for name in ("proposal_id", "parameter_key"):
        id_error = check_id(step[name])
        if id_error:
            return err(id_error)
    if str(step["parameter_key"]).startswith("dec-"):
        return err("ERROR_CODE_FORBIDDEN")
    checked = validated_payload(state, step)
    if "ok" in checked:
        return checked
    payload = checked
    time_error = check_time(step["expiry_unix_seconds"])
    if time_error:
        return err(time_error)
    base = as_int(step["base_policy_version"])
    if base is None:
        return err("ERROR_CODE_MALFORMED")
    gate = business_gate(state, step, tuple(sorted(allowed - {"signer"})))
    if gate:
        return gate
    if step["proposal_id"] in state["proposals"]:
        return err("ERROR_CODE_DUPLICATE_ID")
    if step["expiry_unix_seconds"] <= state["chain_time"]:
        return err("ERROR_CODE_EXPIRED")
    if base != state["policy_version"]:
        return err("ERROR_CODE_STALE_VERSION")
    state["proposals"][step["proposal_id"]] = {
        "proposal_id": step["proposal_id"],
        "parameter_key": step["parameter_key"],
        "base_policy_version": base,
        "payload": payload,
        "payload_digest": step["payload_digest"],
        "state": "OPEN",
        "expiry_unix_seconds": step["expiry_unix_seconds"],
        "execute_after_unix_seconds": 0,
        "approvers": [],
    }
    return finish(state)


def op_approve(state: dict, step: dict) -> dict[str, object]:
    allowed = {"signer", "business_id", "proposal_id"}
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    ready = council_ready(state)
    if ready:
        return err(ready)
    if step["signer"] not in state["seats"]:
        return err("ERROR_CODE_UNAUTHORIZED")
    gate = business_gate(state, step, ("proposal_id", "signer"))
    if gate:
        return gate
    proposal = state["proposals"].get(step["proposal_id"])
    if proposal is None:
        return err("ERROR_CODE_NOT_FOUND")
    if proposal["state"] != "OPEN":
        return err("ERROR_CODE_CONFLICT")
    if state["chain_time"] >= proposal["expiry_unix_seconds"]:
        proposal["state"] = "EXPIRED"
        return keep_error(state, "ERROR_CODE_EXPIRED")
    if step["signer"] in proposal["approvers"]:
        return err("ERROR_CODE_REPLAY")
    proposal["approvers"].append(step["signer"])
    proposal["approvers"].sort()
    required = devnet_param(state, "DEC-008")["numeric_value"]
    delay = devnet_param(state, "DEC-008")["delay_seconds"]
    if len(proposal["approvers"]) >= required:
        proposal["state"] = "QUEUED"
        proposal["execute_after_unix_seconds"] = state["chain_time"] + delay
    return finish(state)


def op_execute(state: dict, step: dict) -> dict[str, object]:
    allowed = {"signer", "business_id", "proposal_id"}
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    role_error = check_role(state, step["signer"], "AUTHORITY")
    if role_error:
        return err(role_error)
    ready = council_ready(state)
    if ready:
        return err(ready)
    gate = business_gate(state, step, ("proposal_id",))
    if gate:
        return gate
    proposal = state["proposals"].get(step["proposal_id"])
    if proposal is None:
        return err("ERROR_CODE_NOT_FOUND")
    if state["chain_time"] >= proposal["expiry_unix_seconds"]:
        proposal["state"] = "EXPIRED"
        return keep_error(state, "ERROR_CODE_EXPIRED")
    if proposal["state"] != "QUEUED":
        return err("ERROR_CODE_CONFLICT")
    if state["chain_time"] < proposal["execute_after_unix_seconds"]:
        return err("ERROR_CODE_OUT_OF_RANGE")
    if proposal["base_policy_version"] != state["policy_version"]:
        previous = state["executed_params"].get(proposal["parameter_key"])
        if previous is not None and previous != proposal["payload_digest"]:
            return err("ERROR_CODE_CONFLICT")
        return err("ERROR_CODE_STALE_VERSION")
    applied = apply_proposal_payload(state, proposal)
    if applied:
        if applied == "ERROR_CODE_FORBIDDEN":
            proposal["state"] = "REJECTED"
            return keep_error(state, applied)
        return err(applied)
    state["executed_params"][proposal["parameter_key"]] = proposal["payload_digest"]
    state["policy_notes"][proposal["parameter_key"]] = proposal["payload_digest"]
    state["policy_version"] += 1
    proposal["state"] = "EXECUTED"
    return finish(
        state,
        {"type": "PolicyChanged", "policy_version": state["policy_version"], "parameter_key": proposal["parameter_key"]},
    )


def op_expire(state: dict, step: dict) -> dict[str, object]:
    bad = unknown(step, {"limit"}) or require_fields(step, ("limit",))
    if bad:
        return bad
    limit = as_int(step["limit"])
    if limit is None or limit < 1:
        return err("ERROR_CODE_BOUNDS")
    cap = min(limit, effective_limit(state, "max_expiry_steps"))
    due: list[tuple[int, str, str]] = []
    for offer_id, offer in state["offers"].items():
        if offer["expiry_unix_seconds"] <= state["chain_time"]:
            due.append((offer["expiry_unix_seconds"], "offer", offer_id))
    for lease_id, lease in state["leases"].items():
        if lease["reservation_held"] and lease["deadline_unix_seconds"] <= state["chain_time"]:
            due.append((lease["deadline_unix_seconds"], "lease", lease_id))
    for evidence_id, evidence in state["evidence"].items():
        if evidence["state"] == "RECORDED" and evidence["expiry_unix_seconds"] <= state["chain_time"]:
            due.append((evidence["expiry_unix_seconds"], "evidence", evidence_id))
    due.sort()
    for _when, kind, object_id in due[:cap]:
        if kind == "offer":
            del state["offers"][object_id]
        elif kind == "evidence":
            state["evidence"][object_id]["state"] = "EXPIRED"
        else:
            lease = state["leases"][object_id]
            provider = state["providers"][lease["provider_id"]]
            release_error = release(provider, lease["package_bytes"])
            if release_error:
                return err(release_error)
            lease["reservation_held"] = False
            lease["state"] = "EXPIRED"
            state["pair_index"].pop(pair_key(lease["provider_id"], lease["content_id"], lease["content_version"]), None)
            state["events"].append({"type": "LeaseEnded", "lease_id": object_id, "state": "EXPIRED"})
    return ok()


def op_commit(state: dict, step: dict) -> dict[str, object]:
    bad = unknown(step, {"header_time"}) or require_fields(step, ("header_time",))
    if bad:
        return bad
    time_error = check_time(step["header_time"])
    if time_error:
        return err(time_error)
    if not commit_possible(state):
        return err("ERROR_CODE_QUORUM")
    if step["header_time"] < state["chain_time"]:
        return err("ERROR_CODE_OUT_OF_RANGE")
    state["chain_time"] = step["header_time"]
    state["height"] += 1
    due_error = apply_due_validator_power(state)
    if due_error:
        return err(due_error)
    return ok()


def op_set_down(state: dict, step: dict) -> dict[str, object]:
    bad = unknown(step, {"validators"}) or require_fields(step, ("validators",))
    if bad:
        return bad
    if not isinstance(step["validators"], list) or any(item not in state["validators"] for item in step["validators"]):
        return err("ERROR_CODE_MALFORMED")
    state["down"] = sorted(set(step["validators"]))
    return ok()


def op_submit(state: dict, step: dict) -> dict[str, object]:
    bad = unknown(step, {"request_id", "tx"}) or require_fields(step, ("request_id", "tx"))
    if bad:
        return bad
    if check_id(step["request_id"]):
        return err("ERROR_CODE_BOUNDS")
    # Hashed invariant only. Liveness is the broadcaster in cases.py.
    if not commit_possible(state):
        state["pending"][step["request_id"]] = {
            "status": "PENDING_STALL",
            "business_id": step["tx"].get("business_id") if isinstance(step["tx"], dict) else "",
        }
        return err("ERROR_CODE_QUORUM", keep=True)
    if not isinstance(step["tx"], dict):
        return err("ERROR_CODE_MALFORMED")
    return dispatch_tx(state, step["tx"])


def op_check_fee(state: dict, step: dict) -> dict[str, object]:
    bad = unknown(step, {"amount", "denom", "gas_limit"}) or require_fields(step, ("amount", "denom", "gas_limit"))
    if bad:
        return bad
    amount = as_int(step["amount"])
    gas = as_int(step["gas_limit"])
    if amount is None or gas is None or not isinstance(step["denom"], str):
        return err("ERROR_CODE_MALFORMED")
    if amount != 0 or step["denom"] != "":
        return err("ERROR_CODE_FEE_REJECTED")
    if gas < BOUNDS["safety_min_gas_per_tx"] or gas > state["safety"]["safety_max_gas_per_tx"]:
        return err("ERROR_CODE_BOUNDS")
    return ok()


def op_create_validator(state: dict, step: dict) -> dict[str, object]:
    bad = unknown(step, set())
    if bad:
        return bad
    return err("ERROR_CODE_FORBIDDEN")


def op_enable_mint(state: dict, step: dict) -> dict[str, object]:
    bad = unknown(step, set())
    if bad:
        return bad
    return err("ERROR_CODE_FORBIDDEN")


def op_publication(state: dict, step: dict) -> dict[str, object]:
    bad = unknown(step, {"content_id", "status"}) or require_fields(step, ("content_id", "status"))
    if bad:
        return bad
    if step["content_id"] not in state["contents"]:
        return err("ERROR_CODE_NOT_FOUND")
    allowed = {
        "PUBLICATION_STATUS_NOT_REQUESTED",
        "PUBLICATION_STATUS_REVIEW_PENDING",
        "PUBLICATION_STATUS_APPROVED",
        "PUBLICATION_STATUS_REJECTED",
        "PUBLICATION_STATUS_DELISTED",
    }
    if step["status"] not in allowed:
        return err("ERROR_CODE_MALFORMED")
    before = state["contents"][step["content_id"]]["state"]
    state["publication"][step["content_id"]] = step["status"]
    if state["contents"][step["content_id"]]["state"] != before:
        return err("ERROR_CODE_CONFLICT")
    return ok()


def op_reachability(state: dict, step: dict) -> dict[str, object]:
    bad = unknown(step, {"provider_id", "status", "lease_id"}) or require_fields(step, ("provider_id", "status"))
    if bad:
        return bad
    if step["provider_id"] not in state["providers"]:
        return err("ERROR_CODE_NOT_FOUND")
    allowed = {
        "REACHABILITY_UNKNOWN",
        "REACHABILITY_REACHABLE",
        "REACHABILITY_DEGRADED",
        "REACHABILITY_UNREACHABLE",
    }
    if step["status"] not in allowed:
        return err("ERROR_CODE_MALFORMED")
    lease_state = None
    if "lease_id" in step:
        lease = state["leases"].get(step["lease_id"])
        if lease is None:
            return err("ERROR_CODE_NOT_FOUND")
        lease_state = lease["state"]
    state["reachability"][step["provider_id"]] = step["status"]
    if lease_state is not None and state["leases"][step["lease_id"]]["state"] != lease_state:
        return err("ERROR_CODE_CONFLICT")
    return ok()


def op_reject_lease_state(state: dict, step: dict) -> dict[str, object]:
    bad = unknown(step, {"state_name"}) or require_fields(step, ("state_name",))
    if bad:
        return bad
    if step["state_name"] in {"LEASE_STATE_AT_RISK", "LEASE_STATE_UNSPECIFIED", "PUBLISHED"}:
        return err("ERROR_CODE_MALFORMED")
    return err("ERROR_CODE_MALFORMED")


def op_approve_decision(state: dict, step: dict) -> dict[str, object]:
    bad = unknown(step, {"decision_id"}) or require_fields(step, ("decision_id",))
    if bad:
        return bad
    if step["decision_id"] not in state["decisions"]:
        return err("ERROR_CODE_NOT_FOUND")
    return err("ERROR_CODE_DECISION_BLOCKED")


def op_check_replica(state: dict, step: dict) -> dict[str, object]:
    bad = unknown(step, {"segment_bytes_hex", "expected_sha256"}) or require_fields(
        step, ("segment_bytes_hex", "expected_sha256")
    )
    if bad:
        return bad
    digest_error = check_digest(step["expected_sha256"])
    if digest_error:
        return err(digest_error)
    try:
        raw = bytes.fromhex(step["segment_bytes_hex"])
    except ValueError:
        return err("ERROR_CODE_MALFORMED")
    if hashlib.sha256(raw).hexdigest() != step["expected_sha256"]:
        return err("ERROR_CODE_INTEGRITY")
    return ok()


def op_fixture_counters(state: dict, step: dict) -> dict[str, object]:
    allowed = {"fixture_hook", "provider_id", "reserved_bytes", "admitted_capacity_bytes"}
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    if step["fixture_hook"] != "NOT_A_TRANSACTION":
        return err("ERROR_CODE_FORBIDDEN")
    provider = state["providers"].get(step["provider_id"])
    if provider is None:
        return err("ERROR_CODE_NOT_FOUND")
    reserved = as_int(step["reserved_bytes"])
    admitted = as_int(step["admitted_capacity_bytes"])
    if reserved is None or admitted is None:
        return err("ERROR_CODE_MALFORMED")
    if reserved < 0 or admitted < 0 or reserved > UINT64_MAX or admitted > UINT64_MAX:
        return err("ERROR_CODE_BOUNDS")
    provider["reserved_bytes"] = reserved
    provider["admitted_capacity_bytes"] = admitted
    return ok()


def op_drain(state: dict, step: dict) -> dict[str, object]:
    allowed = {"signer", "business_id", "provider_id", "provider_signature"}
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    provider = state["providers"].get(step["provider_id"])
    if provider is None:
        return err("ERROR_CODE_NOT_FOUND")
    signature_error = check_signature(step["provider_signature"], DRAIN_DOMAIN, provider["signing_key_id"])
    if signature_error:
        return err(signature_error)
    if step["signer"] != provider["account"]:
        return err("ERROR_CODE_FORBIDDEN")
    gate = business_gate(state, step, ("provider_id", "provider_signature"))
    if gate:
        return gate
    if provider["state"] != "ADMITTED":
        return err("ERROR_CODE_CONFLICT")
    provider["state"] = "DRAINING"
    return finish(state, {"type": "ProviderStateChanged", "provider_id": step["provider_id"], "state": "DRAINING"})


def op_rotate(state: dict, step: dict) -> dict[str, object]:
    allowed = {
        "signer",
        "business_id",
        "provider_id",
        "new_signing_key_id",
        "new_signing_pubkey",
        "expected_generation",
        "current_key_signature",
    }
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    provider = state["providers"].get(step["provider_id"])
    if provider is None:
        return err("ERROR_CODE_NOT_FOUND")
    signature_error = check_signature(step["current_key_signature"], ROTATE_DOMAIN, provider["signing_key_id"])
    if signature_error:
        return err(signature_error)
    if step["signer"] != provider["account"]:
        return err("ERROR_CODE_FORBIDDEN")
    generation = as_int(step["expected_generation"])
    if generation is None:
        return err("ERROR_CODE_MALFORMED")
    id_error = check_id(step["new_signing_key_id"])
    if id_error:
        return err(id_error)
    pubkey_error = check_pubkey(step["new_signing_pubkey"])
    if pubkey_error:
        return err(pubkey_error)
    gate = business_gate(state, step, tuple(sorted(allowed - {"signer"})))
    if gate:
        return gate
    if generation != provider["key_generation"]:
        return err("ERROR_CODE_STALE_VERSION")
    if step["new_signing_key_id"] == provider["signing_key_id"] or step["new_signing_pubkey"] == provider["signing_pubkey"]:
        return err("ERROR_CODE_CONFLICT")
    provider["previous_keys"].append(provider["signing_key_id"])
    provider["previous_pubkeys"].append(provider["signing_pubkey"])
    provider["signing_key_id"] = step["new_signing_key_id"]
    provider["signing_pubkey"] = step["new_signing_pubkey"]
    provider["key_generation"] += 1
    return finish(state, {"type": "ProviderStateChanged", "provider_id": step["provider_id"], "state": provider["state"]})


def op_update_capacity(state: dict, step: dict) -> dict[str, object]:
    allowed = {"signer", "business_id", "provider_id", "declared_capacity_bytes", "endpoint_manifest_digest"}
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    provider = state["providers"].get(step["provider_id"])
    if provider is None:
        return err("ERROR_CODE_NOT_FOUND")
    if step["signer"] != provider["account"]:
        return err("ERROR_CODE_FORBIDDEN")
    capacity = as_int(step["declared_capacity_bytes"])
    if capacity is None:
        return err("ERROR_CODE_MALFORMED")
    if capacity < 1 or capacity > UINT64_MAX:
        return err("ERROR_CODE_BOUNDS")
    digest_error = check_digest(step["endpoint_manifest_digest"])
    if digest_error:
        return err(digest_error)
    gate = business_gate(state, step, ("declared_capacity_bytes", "endpoint_manifest_digest", "provider_id"))
    if gate:
        return gate
    if capacity < provider["reserved_bytes"]:
        return err("ERROR_CODE_CAPACITY")
    provider["admitted_capacity_bytes"] = capacity
    provider["endpoint_manifest_digest"] = step["endpoint_manifest_digest"]
    return finish(state)


def op_disable_provider(state: dict, step: dict) -> dict[str, object]:
    return err("ERROR_CODE_FORBIDDEN")


def op_force_withdraw(state: dict, step: dict) -> dict[str, object]:
    return err("ERROR_CODE_FORBIDDEN")


TX_HANDLERS = {
    "RegisterProvider": op_register_provider,
    "AdmitProvider": op_admit_provider,
    "RegisterContent": op_register_content,
    "WithdrawContent": op_withdraw_content,
    "OfferLease": op_offer,
    "AcceptLease": op_accept,
    "RenewLease": op_renew,
    "EndLease": op_end,
    "SubmitAuditBatch": op_audit,
    "ProposeChange": op_propose,
    "ApproveChange": op_approve,
    "ExecuteChange": op_execute,
    "DrainProvider": op_drain,
    "RotateProviderKey": op_rotate,
    "UpdateProvider": op_update_capacity,
    "DisableProvider": op_disable_provider,
    "ForceWithdraw": op_force_withdraw,
}

# Harness operations drive the specification tests. They are not chain messages
# and they are not reachable from dispatch_tx.
HARNESS_OPS = {
    "ExpireDue": op_expire,
    "CommitBlock": op_commit,
    "SetDown": op_set_down,
    "Submit": op_submit,
    "CheckFee": op_check_fee,
    "MsgCreateValidator": op_create_validator,
    "EnableMint": op_enable_mint,
    "SetPublication": op_publication,
    "SetReachability": op_reachability,
    "RejectLeaseStateName": op_reject_lease_state,
    "ApproveDecision": op_approve_decision,
    "CheckReplica": op_check_replica,
    "FixtureSetCounters": op_fixture_counters,
}


def module_flags() -> dict[str, bool]:
    return {
        "mint": False,
        "distribution": False,
        "slashing": False,
        "staking": False,
        "permissionless_validator_creation": False,
    }


def decision_rows() -> dict[str, dict[str, object]]:
    return {
        decision_id: {"schema_version": 1, "value_version": 0, "approved": False}
        for decision_id in BOUNDS["decisions"]
    }


def safety_caps() -> dict[str, int]:
    return {key: int(BOUNDS[key]) for key in SAFETY_KEYS}


def devnet_state() -> dict:
    state = {
        "chain_id": BOUNDS["example_chain_id"],
        "chain_id_label": BOUNDS["example_chain_id_label"],
        "allow_devnet_test_parameters": True,
        "chain_time": CHAIN_TIME,
        "height": 1,
        "protocol_version": 1,
        "modules": module_flags(),
        "safety": safety_caps(),
        "decisions": decision_rows(),
        "devnet_tests": {
            "DEC-001": {"label": BOUNDS["devnet_test_label"], "approved_policy": False, "role_name": "DEVNET_TEST_ONLY"},
            "DEC-002": {"label": BOUNDS["devnet_test_label"], "approved_policy": False, "role_name": "DEVNET_TEST_ONLY"},
            "DEC-003": {"label": BOUNDS["devnet_test_label"], "approved_policy": False, "role_name": "DEVNET_TEST_ONLY"},
            "DEC-005": {
                "label": BOUNDS["devnet_test_label"],
                "approved_policy": False,
                "numeric_value": TERM_SECONDS,
            },
            "DEC-008": {
                "label": BOUNDS["devnet_test_label"],
                "approved_policy": False,
                "numeric_value": 2,
                "delay_seconds": 10,
            },
        },
        "validators": {"v1": 1, "v2": 1, "v3": 1, "v4": 1},
        "down": [],
        "submitters": {
            SERVICE: {"role": "ADMITTED_SERVICE"},
            BROADCASTER: {"role": "BROADCASTER"},
            PROVIDER_ACCOUNT: {"role": "ADMITTED_PROVIDER"},
            AUDITOR: {"role": "AUDITOR"},
            AUTHORITY: {"role": "AUTHORITY"},
        },
        "seats": ["seat-1", "seat-2", "seat-3"],
        "approved_council": [],
        "validator_pubkeys": {f"v{index}": pubkey_for(f"v{index}") for index in range(1, 5)},
        "validator_reasons": {},
        "pending_power": [],
        "governed_limits": {},
        "providers": {},
        "contents": {},
        "offers": {},
        "leases": {},
        "pair_index": {},
        "evidence": {},
        "batches": set(),
        "proposals": {},
        "executed_params": {},
        "policy_notes": {},
        "policy_version": 1,
        "business": {},
        "pending": {},
        "events": [],
        "publication": {},
        "reachability": {},
        "epoch": 1,
    }
    problems = validate_genesis_view(state)
    if problems:
        raise RuntimeError(problems)
    return state


def production_state() -> dict:
    state = devnet_state()
    state["chain_id"] = "svn-unapproved-example"
    state["allow_devnet_test_parameters"] = False
    state["devnet_tests"] = {}
    state["seats"] = []
    return state


def validate_genesis_view(state: dict) -> list[str]:
    problems: list[str] = []
    for flag, enabled in state["modules"].items():
        if enabled:
            problems.append(f"{flag} enabled")
    if state["protocol_version"] != 1:
        problems.append("protocol_version")
    for decision_id in BOUNDS["decisions"]:
        row = state["decisions"].get(decision_id)
        if row is None or row["approved"] or row["value_version"] != 0:
            problems.append(f"{decision_id} not blocked")
    if state["approved_council"]:
        problems.append("approved council")
    if state["contents"] or state["leases"] or state["offers"]:
        problems.append("genesis catalogue is not empty")
    for key, value in safety_caps().items():
        if state["safety"].get(key) != value:
            problems.append(f"safety {key}")
    if state["allow_devnet_test_parameters"]:
        if not str(state["chain_id"]).startswith(BOUNDS["devnet_chain_id_prefix"]):
            problems.append("devnet flag on a non-dev chain id")
        for row in state["devnet_tests"].values():
            if row.get("label") != BOUNDS["devnet_test_label"] or row.get("approved_policy"):
                problems.append("devnet test parameter mislabeled")
    elif state["devnet_tests"]:
        problems.append("test parameters without the devnet flag")
    powers = list(state["validators"].values())
    if powers and (len(set(powers)) != 1 or powers[0] <= 0):
        problems.append("validator powers")
    if power_set_invariant(state["validators"]):
        problems.append("validator set invariant")
    return problems


def intent(valid: bool = True, role_name: str = "DEVNET_TEST_ONLY", domain: str = HOSTING, key_id: str = "intent-key") -> dict:
    return {"domain": domain, "key_id": key_id, "valid": valid, "role_name": role_name}


def signature(domain: str, key_id: str, valid: bool = True, observation_digest: str | None = None) -> dict:
    signed = {"domain": domain, "key_id": key_id, "valid": valid}
    if observation_digest is not None:
        signed["observation_digest"] = observation_digest
    return signed


# Off-chain or transient. Reachability and publication are not chain state.
# `down` is an environment observation. DEC-011 keeps it out of the application
# hash. commit_possible and power-change scheduling do not read it.
# The outbox reads it only to withhold a block. `pending` is that outbox.
# `events` are derived. `_pending_business` is discarded on failure.
APP_HASH_EXCLUDED = (
    "_pending_business",
    "down",
    "events",
    "pending",
    "publication",
    "reachability",
)


def json_ready(value: object) -> object:
    if isinstance(value, dict):
        ready: dict[str, object] = {}
        for key in sorted(value):
            item = value[key]
            if item is None:
                continue
            ready[str(key)] = json_ready(item)
        return ready
    if isinstance(value, set):
        items = [json_ready(item) for item in value]
        return sorted(items, key=lambda item: json.dumps(item, sort_keys=True, separators=(",", ":")))
    if isinstance(value, (list, tuple)):
        return [json_ready(item) for item in value]
    return value


def chain_view(state: dict) -> dict:
    """Canonical chain-state projection. Off-chain records are omitted.

    `pair_index` keys use `|`, which SVN-JCS-1 rejects, so the projection
    stores that index as a sorted list of records.
    """
    view = {key: state[key] for key in state if key not in APP_HASH_EXCLUDED}
    pairs = []
    for key, lease_id in sorted(state["pair_index"].items()):
        provider_id, content_id, version = key.split("|")
        pairs.append(
            {
                "content_id": content_id,
                "content_version": int(version),
                "lease_id": lease_id,
                "provider_id": provider_id,
            }
        )
    view["pair_index"] = pairs
    ready = json_ready(view)
    if not isinstance(ready, dict):
        raise ValueError("chain view")
    return ready


def app_hash(state: dict) -> str:
    return hashlib.sha256(canonicalize(chain_view(state)).encode("utf-8")).hexdigest()


def register_provider(provider_id: str = "p1", capacity: int = 1_000_000, group: str = "devnet-test-only") -> dict:
    return {
        "op": "RegisterProvider",
        "signer": PROVIDER_ACCOUNT,
        "business_id": f"reg-{provider_id}",
        "provider_id": provider_id,
        "signing_key_id": f"key-{provider_id}",
        "signing_pubkey": pubkey_for(provider_id),
        "endpoint_manifest_digest": ENDPOINT,
        "declared_capacity_bytes": capacity,
        "operator_group_id": group,
    }


def admit_provider(provider_id: str = "p1") -> dict:
    return {
        "op": "AdmitProvider",
        "signer": AUTHORITY,
        "business_id": f"admit-{provider_id}",
        "provider_id": provider_id,
    }


def register_content(content_id: str = "c1", package_bytes: int = PACKAGE_BYTES) -> dict:
    return {
        "op": "RegisterContent",
        "signer": SERVICE,
        "business_id": f"content-{content_id}",
        "content_id": content_id,
        "content_version": 1,
        "package_bytes": package_bytes,
        "manifest_digest": MANIFEST,
        "hosting_intent": intent(),
    }


def offer(
    offer_id: str = "o1",
    provider_id: str = "p1",
    content_id: str = "c1",
    package_bytes: int = PACKAGE_BYTES,
    expiry: int | None = None,
) -> dict:
    start = CHAIN_TIME
    return {
        "op": "OfferLease",
        "signer": SERVICE,
        "business_id": f"offer-{offer_id}",
        "offer_id": offer_id,
        "content_id": content_id,
        "content_version": 1,
        "provider_id": provider_id,
        "package_bytes": package_bytes,
        "manifest_digest": MANIFEST,
        "terms_digest": TERMS,
        "start_unix_seconds": start,
        "deadline_unix_seconds": start + TERM_SECONDS,
        "expiry_unix_seconds": CHAIN_TIME + 500 if expiry is None else expiry,
        "term_version": 1,
    }


def governed_limit_payload(limit_key: str, value: int) -> dict:
    return {"governedLimit": {"limitKey": limit_key, "value": str(value)}}


def validator_power_payload(node_id: str, power: int, activation: dict[str, str], pubkey_hex: str | None = None) -> dict:
    body = {
        "activation": activation,
        "consensusPubkey": standard_b64(bytes.fromhex(pubkey_hex or pubkey_for(node_id))),
        "nodeId": node_id,
        "reasonDigest": digest_json(REASON),
    }
    if power != 0:
        body["power"] = str(power)
    return {"validatorPower": body}


def admit_change_payload(provider_id: str) -> dict:
    return {"admitProvider": {"providerId": provider_id}}


def disable_change_payload(provider_id: str) -> dict:
    return {"disableProvider": {"providerId": provider_id}}


def force_withdraw_payload(content_id: str, version: int = 1, reason_hex: str = REASON) -> dict:
    return {
        "forceWithdraw": {
            "contentId": content_id,
            "contentVersion": str(version),
            "reasonDigest": digest_json(reason_hex),
        }
    }


def proposal_step(proposal_id: str, parameter_key: str, payload: dict, base: int = 1, signer: str = "seat-1") -> dict:
    return {
        "op": "ProposeChange",
        "signer": signer,
        "business_id": f"prop-{proposal_id}",
        "proposal_id": proposal_id,
        "parameter_key": parameter_key,
        "base_policy_version": base,
        "payload": payload,
        "payload_digest": payload_digest(payload),
        "expiry_unix_seconds": CHAIN_TIME + 10000,
    }


def accept(offer_id: str = "o1", lease_id: str = "l1", business_id: str | None = None, valid: bool = True, domain: str | None = None, key_id: str | None = None) -> dict:
    return {
        "op": "AcceptLease",
        "signer": BROADCASTER,
        "business_id": business_id or f"accept-{lease_id}",
        "offer_id": offer_id,
        "lease_id": lease_id,
        "provider_acceptance": signature(domain or ACCEPT_DOMAIN, key_id or "key-p1", valid),
        "tx_hash": f"tx-{lease_id}",
    }
