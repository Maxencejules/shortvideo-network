"""Executable specification of protocol v0 transitions.

This is not a consensus implementation and it is not a chain. It checks the
invariants Goal 02 freezes for later modules. It does not read a clock, the
network, or the filesystem except for the bounds file imported below.
DEC-001 through DEC-008 cannot be marked approved by any transition.
"""

from __future__ import annotations

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
    op = step.get("op")
    if not isinstance(op, str):
        return err("ERROR_CODE_MALFORMED")
    handler = HANDLERS.get(op)
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


def check_package(value: object, state: dict) -> str | None:
    number = as_int(value)
    if number is None:
        return "ERROR_CODE_MALFORMED"
    if number < 1 or number > state["safety"]["safety_max_package_bytes"]:
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


def check_signature(sig: object, domain: str, key_id: str) -> str | None:
    if not isinstance(sig, dict):
        return "ERROR_CODE_MALFORMED"
    extra = set(sig) - {"domain", "key_id", "valid", "role_name"}
    if extra:
        return "ERROR_CODE_UNKNOWN_FIELD"
    if sig.get("domain") != domain:
        return "ERROR_CODE_WRONG_DOMAIN"
    if sig.get("valid") is not True:
        return "ERROR_CODE_UNAUTHORIZED"
    if sig.get("key_id") != key_id:
        return "ERROR_CODE_UNAUTHORIZED"
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


def commit_possible(state: dict) -> bool:
    total = 0
    available = 0
    for validator, power in sorted(state["validators"].items()):
        if not isinstance(power, int) or isinstance(power, bool) or power <= 0:
            return False
        total += power
        if validator not in state["down"]:
            available += power
    if total <= 0 or total > SAFE_MAX:
        return False
    return available * 3 > total * 2


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
        "key_generation": 1,
        "state": "APPLIED",
        "admitted_capacity_bytes": capacity,
        "reserved_bytes": 0,
        "operator_group_id": step["operator_group_id"],
        "previous_keys": [],
    }
    return finish(state, {"type": "ProviderRegistered", "provider_id": step["provider_id"], "state": "APPLIED"})


def op_admit_provider(state: dict, step: dict) -> dict[str, object]:
    allowed = {"signer", "business_id", "provider_id"}
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    role_error = check_role(state, step["signer"], "AUTHORITY")
    if role_error:
        return err(role_error)
    id_error = check_id(step["provider_id"])
    if id_error:
        return err(id_error)
    gate = business_gate(state, step, ("provider_id",))
    if gate:
        return gate
    provider = state["providers"].get(step["provider_id"])
    if provider is None:
        return err("ERROR_CODE_NOT_FOUND")
    param = devnet_param(state, "DEC-002")
    if param is None or param.get("role_name") != "DEVNET_TEST_ONLY":
        return err("ERROR_CODE_DECISION_BLOCKED")
    if provider["operator_group_id"] != "devnet-test-only":
        return err("ERROR_CODE_FORBIDDEN")
    if provider["state"] != "APPLIED":
        return err("ERROR_CODE_CONFLICT")
    provider["state"] = "ADMITTED"
    return finish(state, {"type": "ProviderStateChanged", "provider_id": step["provider_id"], "state": "ADMITTED"})


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


def op_withdraw_content(state: dict, step: dict) -> dict[str, object]:
    allowed = {"signer", "business_id", "content_id", "content_version", "hosting_intent"}
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    role_error = check_role(state, step["signer"], "ADMITTED_SERVICE")
    if role_error:
        return err(role_error)
    intent_error = intent_ok(state, step["hosting_intent"])
    if intent_error:
        return err(intent_error)
    gate = business_gate(state, step, ("content_id", "content_version", "hosting_intent"))
    if gate:
        return gate
    content = state["contents"].get(step["content_id"])
    if content is None:
        return err("ERROR_CODE_NOT_FOUND")
    if content["submitting_host"] != step["signer"]:
        return err("ERROR_CODE_FORBIDDEN")
    if content["state"] == "WITHDRAWN":
        return err("ERROR_CODE_WITHDRAWN")
    if step["content_version"] != content["content_version"]:
        return err("ERROR_CODE_STALE_VERSION")
    content["state"] = "WITHDRAWN"
    content["tombstone"] = {
        "history_erased": False,
        "retention_decision_id": "DEC-004",
        "retention_value_version": 0,
        "committed_unix_seconds": state["chain_time"],
    }
    for offer_id in [offer_id for offer_id, offer in state["offers"].items() if offer["content_id"] == step["content_id"]]:
        del state["offers"][offer_id]
    return finish(state, {"type": "ContentWithdrawn", "content_id": step["content_id"], "history_erased": False})


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
    if open_offers >= state["safety"]["safety_max_open_offers_per_provider"]:
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
        "provider_signature",
        "evidence_id",
    }
    bad = unknown(step, allowed) or require_fields(
        step,
        ("signer", "business_id", "lease_id", "term_version", "deadline_unix_seconds", "package_bytes", "provider_signature"),
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
    gate = business_gate(
        state,
        step,
        ("deadline_unix_seconds", "evidence_id", "lease_id", "package_bytes", "provider_signature", "term_version"),
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
    if step["package_bytes"] != lease["package_bytes"]:
        return err("ERROR_CODE_CONFLICT")
    time_error = check_time(step["deadline_unix_seconds"])
    if time_error:
        return err(time_error)
    extension = step["deadline_unix_seconds"] - lease["deadline_unix_seconds"]
    term_error = sponsorship_seconds(state, extension)
    if term_error:
        return err(term_error)
    if "evidence_id" in step:
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
    }
    bad = unknown(step, allowed) or require_fields(step, tuple(sorted(allowed)))
    if bad:
        return bad
    role_error = check_role(state, step["signer"], "AUDITOR")
    if role_error:
        return err(role_error)
    signature_error = check_signature(step["auditor_signature"], EVIDENCE_DOMAIN, AUDITOR_KEY)
    if signature_error:
        return err(signature_error)
    for name in ("batch_id", "evidence_id", "lease_id", "content_id"):
        id_error = check_id(step[name])
        if id_error:
            return err(id_error)
    if step["kind"] not in {"PROVIDER_RECEIPT", "AUDITOR_ATTESTATION"}:
        return err("ERROR_CODE_MALFORMED")
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
    state["evidence"][step["evidence_id"]] = {
        "batch_id": step["batch_id"],
        "kind": step["kind"],
        "state": "RECORDED",
        "lease_id": step["lease_id"],
        "content_id": step["content_id"],
        "expiry_unix_seconds": step["expiry_unix_seconds"],
        "attestation": True,
    }
    state["batches"].add(step["batch_id"])
    if lease["deadline_unix_seconds"] != deadline_before or lease["state"] != state_before:
        return err("ERROR_CODE_CONFLICT")
    return finish(
        state,
        {"type": "EvidenceAccepted", "evidence_id": step["evidence_id"], "attestation": True, "kind": step["kind"]},
    )


def op_propose(state: dict, step: dict) -> dict[str, object]:
    allowed = {"signer", "business_id", "proposal_id", "parameter_key", "base_policy_version", "payload_digest", "expiry_unix_seconds"}
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
    digest_error = check_digest(step["payload_digest"])
    if digest_error:
        return err(digest_error)
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
        return err("ERROR_CODE_EXPIRED")
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
        return err("ERROR_CODE_EXPIRED")
    if proposal["state"] != "QUEUED":
        return err("ERROR_CODE_CONFLICT")
    if state["chain_time"] < proposal["execute_after_unix_seconds"]:
        return err("ERROR_CODE_OUT_OF_RANGE")
    if proposal["base_policy_version"] != state["policy_version"]:
        previous = state["executed_params"].get(proposal["parameter_key"])
        if previous is not None and previous != proposal["payload_digest"]:
            return err("ERROR_CODE_CONFLICT")
        return err("ERROR_CODE_STALE_VERSION")
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
    cap = min(limit, state["safety"]["safety_max_expiry_steps_per_block"])
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
    if not commit_possible(state):
        state["pending"][step["request_id"]] = {
            "status": "PENDING_STALL",
            "business_id": step["tx"].get("business_id") if isinstance(step["tx"], dict) else "",
        }
        return err("ERROR_CODE_QUORUM", keep=True)
    if not isinstance(step["tx"], dict):
        return err("ERROR_CODE_MALFORMED")
    return dispatch(state, step["tx"])


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
    allowed = {"signer", "business_id", "provider_id", "new_signing_key_id", "expected_generation", "current_key_signature"}
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
    gate = business_gate(state, step, ("expected_generation", "new_signing_key_id", "provider_id", "current_key_signature"))
    if gate:
        return gate
    if generation != provider["key_generation"]:
        return err("ERROR_CODE_STALE_VERSION")
    if step["new_signing_key_id"] == provider["signing_key_id"]:
        return err("ERROR_CODE_CONFLICT")
    provider["previous_keys"].append(provider["signing_key_id"])
    provider["signing_key_id"] = step["new_signing_key_id"]
    provider["key_generation"] += 1
    return finish(state, {"type": "ProviderStateChanged", "provider_id": step["provider_id"], "state": provider["state"]})


def op_update_capacity(state: dict, step: dict) -> dict[str, object]:
    allowed = {"signer", "business_id", "provider_id", "declared_capacity_bytes"}
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
    gate = business_gate(state, step, ("declared_capacity_bytes", "provider_id"))
    if gate:
        return gate
    if capacity < provider["reserved_bytes"]:
        return err("ERROR_CODE_CAPACITY")
    provider["admitted_capacity_bytes"] = capacity
    return finish(state)


HANDLERS = {
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
    "DrainProvider": op_drain,
    "RotateProviderKey": op_rotate,
    "UpdateProvider": op_update_capacity,
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
    return problems


def intent(valid: bool = True, role_name: str = "DEVNET_TEST_ONLY", domain: str = HOSTING, key_id: str = "intent-key") -> dict:
    return {"domain": domain, "key_id": key_id, "valid": valid, "role_name": role_name}


def signature(domain: str, key_id: str, valid: bool = True) -> dict:
    return {"domain": domain, "key_id": key_id, "valid": valid}


def register_provider(provider_id: str = "p1", capacity: int = 1_000_000, group: str = "devnet-test-only") -> dict:
    return {
        "op": "RegisterProvider",
        "signer": PROVIDER_ACCOUNT,
        "business_id": f"reg-{provider_id}",
        "provider_id": provider_id,
        "signing_key_id": f"key-{provider_id}",
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
