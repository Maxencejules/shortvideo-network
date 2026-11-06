#!/usr/bin/env python3
"""Differential oracle for chain/x/leases (goal 08, keeper level, not end to end).

It drives docs/evidence/goal-02/spec_machine.py, the executable protocol v0
specification, one step at a time for the Go tests in chain/x/leases/difftest
(it lives under tests/ because the goal-01 contract checker allows Python
only in the repository's Python trees). It is
test support. It does not run in chain execution and it is not evidence of a
committed transaction.

Run it as ``python -I spec_oracle.py --stream`` (the Go tests do) or
``python -I spec_oracle.py --self-test``. It uses only the standard library
and the two goal-02 modules (spec_machine.py, jcs.py). It reads no clock,
randomness, network, or file other than those modules and bounds.json.

Stream protocol: one JSON object per line on stdin, one JSON object per line
on stdout.

    {"cmd": "reset", "config": {...}, "disable_deviations": false}
    {"cmd": "step", "step": {...}}

reset config: chain_time, term_seconds (an integer, or null for a chain
without the DEC-005 devnet number), governed (governed limit values),
submitters (lowercase hex account -> role name),
max_offer_lifetime_seconds, max_open_offers_per_content, and
max_open_offers_per_content_per_scheduler (the chain constants of deviations
D-5, D-6, and D-10).

A transaction step is a spec_machine step dict (OfferLease, AcceptLease,
RenewLease, EndLease, RegisterProvider, UpdateProvider, RotateProviderKey,
DrainProvider, RegisterContent, WithdrawContent). It goes to
spec_machine.dispatch_tx on a copy of the state, which is kept only when the
step succeeds. A fixture step is not a transaction: Block (CommitBlock, then
ExpireDue with the max_expiry_steps ceiling), CouncilAdmit, CouncilDisable,
CouncilForceWithdraw (apply_proposal_payload, kept only on success), and
SeedProvider (a direct provider record insert, like cases.py seed_admitted).

An EndLease provider_signature may carry "term_version", the lease term
version the Go side's signature binds (chain/x/leases payload
SVN-PROVIDER-END-1). It is not a spec_machine.py field: the adapter always
removes it before the step reaches spec_machine.py.

The step response is {"code", "idempotent", "events", "deviation",
"projection"}. code is "ok" or an ERROR_CODE_* name. events lists the
svn.events.v1 events of the step as {"type", "id"}. deviation names the chain
deviation rule (D-2 to D-6 and D-10 to D-13 of chain/x/leases/README.md) that
changed the spec's outcome into the chain's rejection, or null. D-2 to D-6 and
D-10 apply only to a non-idempotent spec success, in the chain's order; D-11
applies only to an idempotent OfferLease success whose signer is not the
scheduler that committed the offer; D-13 only to an idempotent AcceptLease
success whose ADMITTED_PROVIDER signer is not the account of the lease's
provider. D-12 marks a provider EndLease signature that spec_machine.py's
check_signature would accept but that binds another term version than the
lease's current one as invalid, so the step is UNAUTHORIZED at the signature
check. Every rule discards the step. No other semantics live in this
adapter. A request it cannot read returns {"error": "..."}.
"""

from __future__ import annotations

import argparse
import copy
import json
import sys
from pathlib import Path
from typing import Any, Final

GOAL02: Final = Path(__file__).resolve().parents[2] / "docs" / "evidence" / "goal-02"
sys.path.insert(0, str(GOAL02))

import spec_machine as sm  # noqa: E402

JSON = dict[str, Any]
# A self-test case: (step, expected code, expected deviation rule).
Case = tuple[JSON, str, str | None]

TX_OPS: Final = frozenset(
    {
        "OfferLease",
        "AcceptLease",
        "RenewLease",
        "EndLease",
        "RegisterProvider",
        "UpdateProvider",
        "RotateProviderKey",
        "DrainProvider",
        "RegisterContent",
        "WithdrawContent",
    }
)
ROLES: Final = frozenset(
    {"ADMITTED_SERVICE", "BROADCASTER", "ADMITTED_PROVIDER", "AUDITOR", "AUTHORITY"}
)
PROVIDER_STATES: Final = frozenset({"APPLIED", "ADMITTED", "DRAINING", "DISABLED"})
# The id an event is reported by: the first of these fields it carries.
EVENT_ID_FIELDS: Final = ("lease_id", "offer_id", "provider_id", "content_id")
CONFIG_KEYS: Final = frozenset(
    {
        "chain_time",
        "term_seconds",
        "governed",
        "submitters",
        "max_offer_lifetime_seconds",
        "max_open_offers_per_content",
        "max_open_offers_per_content_per_scheduler",
    }
)
SEED_KEYS: Final = frozenset(
    {
        "op",
        "provider_id",
        "account",
        "signing_key_id",
        "signing_pubkey",
        "endpoint_manifest_digest",
        "state",
        "admitted_capacity_bytes",
        "reserved_bytes",
        "operator_group_id",
    }
)
DUPLICATE_ID: Final = "ERROR_CODE_DUPLICATE_ID"
BOUNDS_CODE: Final = "ERROR_CODE_BOUNDS"
FORBIDDEN: Final = "ERROR_CODE_FORBIDDEN"
EXPIRED: Final = "ERROR_CODE_EXPIRED"
UNAUTHORIZED: Final = "ERROR_CODE_UNAUTHORIZED"


class RequestError(ValueError):
    """A request this adapter cannot read. It is a harness bug, not a code."""


def need_int(value: object, name: str, low: int, high: int) -> int:
    if isinstance(value, bool) or not isinstance(value, int):
        raise RequestError(f"{name} must be an integer")
    if value < low or value > high:
        raise RequestError(f"{name} {value} outside [{low}, {high}]")
    return value


def need_str(value: object, name: str) -> str:
    if not isinstance(value, str) or value == "":
        raise RequestError(f"{name} must be a non-empty string")
    return value


def need_keys(obj: JSON, allowed: frozenset[str], name: str) -> None:
    extra = set(obj) - allowed
    missing = allowed - set(obj)
    if extra or missing:
        raise RequestError(f"{name}: extra {sorted(extra)}, missing {sorted(missing)}")


def need_dict(value: object, name: str) -> JSON:
    if not isinstance(value, dict):
        raise RequestError(f"{name} must be an object")
    return value


def event_ref(event: JSON) -> JSON:
    for field in EVENT_ID_FIELDS:
        value = event.get(field)
        if isinstance(value, str) and value:
            return {"type": str(event["type"]), "id": value}
    raise RequestError(f"event {event.get('type')} carries no id field")


def projection(state: JSON) -> JSON:
    """The chain-state projection both sides compare (plan section 14.3).

    Integers are decimal strings so uint64 values survive JSON exactly.
    """
    providers = []
    for provider_id in sorted(state["providers"]):
        p = state["providers"][provider_id]
        providers.append(
            {
                "provider_id": provider_id,
                "account": p["account"],
                "state": p["state"],
                "signing_key_id": p["signing_key_id"],
                "signing_pubkey": p["signing_pubkey"],
                "key_generation": str(p["key_generation"]),
                "admitted_capacity_bytes": str(p["admitted_capacity_bytes"]),
                "reserved_bytes": str(p["reserved_bytes"]),
                "operator_group_id": p["operator_group_id"],
                "endpoint_manifest_digest": p["endpoint_manifest_digest"],
                "previous_pubkeys": list(p["previous_pubkeys"]),
            }
        )
    contents = []
    for content_id in sorted(state["contents"]):
        c = state["contents"][content_id]
        tomb = c.get("tombstone")
        contents.append(
            {
                "content_id": content_id,
                "content_version": str(c["content_version"]),
                "manifest_digest": c["manifest_digest"],
                "package_bytes": str(c["package_bytes"]),
                "submitting_host": c["submitting_host"],
                "state": c["state"],
                "tombstone": None
                if tomb is None
                else {
                    "committed_unix_seconds": str(tomb["committed_unix_seconds"]),
                    "reason_digest": tomb["reason_digest"],
                },
            }
        )
    offers = []
    for offer_id in sorted(state["offers"]):
        o = state["offers"][offer_id]
        offers.append(
            {
                "offer_id": offer_id,
                "content_id": o["content_id"],
                "content_version": str(o["content_version"]),
                "provider_id": o["provider_id"],
                "package_bytes": str(o["package_bytes"]),
                "manifest_digest": o["manifest_digest"],
                "terms_digest": o["terms_digest"],
                "start_unix_seconds": str(o["start_unix_seconds"]),
                "deadline_unix_seconds": str(o["deadline_unix_seconds"]),
                "expiry_unix_seconds": str(o["expiry_unix_seconds"]),
                "term_version": str(o["term_version"]),
                "scheduler": o["scheduler"],
            }
        )
    leases = []
    for lease_id in sorted(state["leases"]):
        lease = state["leases"][lease_id]
        leases.append(
            {
                "lease_id": lease_id,
                "offer_id": lease["offer_id"],
                "content_id": lease["content_id"],
                "content_version": str(lease["content_version"]),
                "provider_id": lease["provider_id"],
                "package_bytes": str(lease["package_bytes"]),
                "manifest_digest": lease["manifest_digest"],
                "terms_digest": lease["terms_digest"],
                "start_unix_seconds": str(lease["start_unix_seconds"]),
                "deadline_unix_seconds": str(lease["deadline_unix_seconds"]),
                "state": lease["state"],
                "term_version": str(lease["term_version"]),
                "policy_version": str(lease["policy_version"]),
                "reservation_held": bool(lease["reservation_held"]),
            }
        )
    pairs = []
    for key, lease_id in state["pair_index"].items():
        provider_id, content_id, version = key.split("|")
        pairs.append(
            {
                "provider_id": provider_id,
                "content_id": content_id,
                "content_version": version,
                "lease_id": lease_id,
            }
        )
    pairs.sort(key=lambda p: (p["provider_id"], p["content_id"], int(p["content_version"])))
    return {
        "chain_time": str(state["chain_time"]),
        "providers": providers,
        "contents": contents,
        "offers": offers,
        "leases": leases,
        "pairs": pairs,
        "business_ids": sorted(state["business"]),
    }


class Oracle:
    """spec_machine.py state plus the chain deviation rules D-2 to D-6, D-10 to D-13."""

    def __init__(self) -> None:
        self.state: JSON | None = None
        # Every offer id an OfferLease committed, with its scheduler (the
        # chain's used-offer marker).
        self.used_offer_ids: dict[str, str] = {}
        self.deviations = True
        self.max_offer_lifetime = 0
        self.max_open_offers_per_content = 0
        self.max_open_offers_per_content_per_scheduler = 0

    def reset(self, config: JSON, disable_deviations: bool) -> JSON:
        need_keys(config, CONFIG_KEYS, "config")
        state = sm.devnet_state()
        state["chain_time"] = need_int(
            config["chain_time"], "chain_time", 1, int(sm.BOUNDS["unix_seconds_max"])
        )
        term = config["term_seconds"]
        if term is None:
            del state["devnet_tests"]["DEC-005"]
        else:
            state["devnet_tests"]["DEC-005"]["numeric_value"] = need_int(
                term, "term_seconds", 1, sm.SAFE_MAX
            )
        caps = sm.BOUNDS["governed_limit_caps"]
        governed: JSON = {}
        for key, value in need_dict(config["governed"], "governed").items():
            if key not in caps:
                raise RequestError(f"unknown governed limit {key}")
            governed[key] = need_int(value, key, 1, int(caps[key]))
        state["governed_limits"] = governed
        submitters: JSON = {}
        for account, role in need_dict(config["submitters"], "submitters").items():
            if sm.check_address(account) is not None:
                raise RequestError(f"submitter {account} is not a 20-byte hex account")
            if role not in ROLES:
                raise RequestError(f"submitter role {role}")
            submitters[account] = {"role": role}
        state["submitters"] = submitters
        self.max_offer_lifetime = need_int(
            config["max_offer_lifetime_seconds"], "max_offer_lifetime_seconds", 1, sm.SAFE_MAX
        )
        self.max_open_offers_per_content = need_int(
            config["max_open_offers_per_content"], "max_open_offers_per_content", 1, sm.SAFE_MAX
        )
        self.max_open_offers_per_content_per_scheduler = need_int(
            config["max_open_offers_per_content_per_scheduler"],
            "max_open_offers_per_content_per_scheduler",
            1,
            sm.SAFE_MAX,
        )
        if not isinstance(disable_deviations, bool):
            raise RequestError("disable_deviations must be a boolean")
        self.deviations = not disable_deviations
        self.used_offer_ids = {}
        self.state = state
        return {"ok": True, "projection": projection(state)}

    def current(self) -> JSON:
        if self.state is None:
            raise RequestError("step before reset")
        return self.state

    def step(self, step: JSON) -> JSON:
        op = step.get("op")
        if op in TX_OPS:
            return self.transaction(step)
        if op == "Block":
            return self.block(step)
        if op in ("CouncilAdmit", "CouncilDisable", "CouncilForceWithdraw"):
            return self.council(step)
        if op == "SeedProvider":
            return self.seed(step)
        raise RequestError(f"unknown op {op!r}")

    def respond(
        self, code: str, idempotent: bool, events: list[JSON], deviation: str | None
    ) -> JSON:
        return {
            "code": code,
            "idempotent": idempotent,
            "events": [event_ref(event) for event in events],
            "deviation": deviation,
            "projection": projection(self.current()),
        }

    def transaction(self, step: JSON) -> JSON:
        pre = self.current()
        before = len(pre["events"])
        step, stale_end = self.end_binding(pre, step)
        trial = copy.deepcopy(pre)
        result = sm.dispatch_tx(trial, step)
        ok = bool(result["ok"])
        idempotent = bool(result["idempotent"])
        code = "ok" if ok else str(result["error"])
        if stale_end:
            if code != UNAUTHORIZED:
                raise RequestError(f"D-12 signature reached {code}, not the signature check")
            return self.respond(code, False, [], "D-12")
        deviation = None
        if ok and not idempotent and self.deviations:
            rule = self.deviation(pre, step)
            if rule is not None:
                deviation, code = rule
        if ok and idempotent and self.deviations and step["op"] == "OfferLease":
            # D-11: the idempotent success goes only to the offer's scheduler.
            if self.used_offer_ids.get(step["offer_id"]) != step["signer"]:
                return self.respond(FORBIDDEN, False, [], "D-11")
        if ok and idempotent and self.deviations and step["op"] == "AcceptLease":
            # D-13: an ADMITTED_PROVIDER account gets the idempotent success
            # only for its own provider's lease; a BROADCASTER relays for all.
            if not self.accept_replay_allowed(pre, step):
                return self.respond(FORBIDDEN, False, [], "D-13")
        if not result["keep"] or deviation is not None:
            return self.respond(code, False, [], deviation)
        self.state = trial
        if step["op"] == "OfferLease" and ok and not idempotent:
            self.used_offer_ids[step["offer_id"]] = step["signer"]
        return self.respond(code, idempotent, trial["events"][before:], None)

    def end_binding(self, pre: JSON, step: JSON) -> tuple[JSON, bool]:
        """D-12: a provider end signature binds the lease's term version.

        Returns the step without the "term_version" signature field, and
        whether the rule marked the signature invalid: rules on, a provider
        end of an existing lease, a signature check_signature would accept,
        and a term version other than the lease's current one.
        """
        sig = step.get("provider_signature")
        if step.get("op") != "EndLease" or not isinstance(sig, dict) or "term_version" not in sig:
            return step, False
        signed = need_int(sig["term_version"], "term_version", 1, sm.SAFE_MAX)
        plain = {name: value for name, value in sig.items() if name != "term_version"}
        step = {**step, "provider_signature": plain}
        if not self.deviations or step.get("authority_end") is not False:
            return step, False
        lease = pre["leases"].get(step.get("lease_id"))
        if lease is None or signed == lease["term_version"]:
            return step, False
        provider = pre["providers"][lease["provider_id"]]
        if sm.check_signature(plain, sm.END_DOMAIN, provider["signing_key_id"]) is not None:
            return step, False
        step["provider_signature"] = {**plain, "valid": False}
        return step, True

    def accept_replay_allowed(self, pre: JSON, step: JSON) -> bool:
        """D-13: who gets an idempotent AcceptLease success."""
        record = pre["submitters"].get(step["signer"])
        if record is None or record["role"] != "ADMITTED_PROVIDER":
            return True
        lease = pre["leases"].get(step["lease_id"])
        if lease is None:
            return False
        return bool(pre["providers"][lease["provider_id"]]["account"] == step["signer"])

    def deviation(self, pre: JSON, step: JSON) -> tuple[str, str] | None:
        """The chain-only checks that run after every spec check (README D-2..D-6, D-10)."""
        now = pre["chain_time"]
        op = step["op"]
        if op == "OfferLease":
            if step["offer_id"] in self.used_offer_ids:
                return "D-4", DUPLICATE_ID
            if step["expiry_unix_seconds"] - now > self.max_offer_lifetime:
                return "D-5", BOUNDS_CODE
            content_id = step["content_id"]
            open_offers = sum(1 for o in pre["offers"].values() if o["content_id"] == content_id)
            if open_offers >= self.max_open_offers_per_content:
                return "D-6", BOUNDS_CODE
            own = sum(
                1
                for o in pre["offers"].values()
                if o["content_id"] == content_id and o["scheduler"] == step["signer"]
            )
            if own >= self.max_open_offers_per_content_per_scheduler:
                return "D-10", BOUNDS_CODE
        elif op == "AcceptLease":
            if pre["offers"][step["offer_id"]]["deadline_unix_seconds"] <= now:
                return "D-3", EXPIRED
        elif op == "RenewLease":
            if pre["leases"][step["lease_id"]]["deadline_unix_seconds"] <= now:
                return "D-2", EXPIRED
        return None

    def block(self, step: JSON) -> JSON:
        need_keys(step, frozenset({"op", "header_time"}), "Block")
        header_time = need_int(step["header_time"], "header_time", 1, sm.SAFE_MAX)
        pre = self.current()
        before = len(pre["events"])
        trial = copy.deepcopy(pre)
        result = sm.dispatch(trial, {"op": "CommitBlock", "header_time": header_time})
        if result["ok"]:
            ceiling = int(sm.BOUNDS["governed_limit_caps"]["max_expiry_steps"])
            result = sm.dispatch(trial, {"op": "ExpireDue", "limit": ceiling})
        if not result["ok"]:
            return self.respond(str(result["error"]), False, [], None)
        self.state = trial
        return self.respond("ok", False, trial["events"][before:], None)

    def council(self, step: JSON) -> JSON:
        op = step["op"]
        payload: JSON
        if op == "CouncilForceWithdraw":
            keys = frozenset({"op", "content_id", "content_version", "reason_digest"})
            need_keys(step, keys, op)
            payload = {
                "kind": "force_withdraw",
                "content_id": need_str(step["content_id"], "content_id"),
                "content_version": need_int(
                    step["content_version"], "content_version", 0, int(sm.UINT64_MAX)
                ),
                "reason_digest": need_str(step["reason_digest"], "reason_digest"),
            }
        else:
            need_keys(step, frozenset({"op", "provider_id"}), op)
            kind = "admit_provider" if op == "CouncilAdmit" else "disable_provider"
            payload = {"kind": kind, "provider_id": need_str(step["provider_id"], "provider_id")}
        trial = copy.deepcopy(self.current())
        error = sm.apply_proposal_payload(trial, {"payload": payload})
        if error is not None:
            return self.respond(str(error), False, [], None)
        self.state = trial
        return self.respond("ok", False, [], None)

    def seed(self, step: JSON) -> JSON:
        need_keys(step, SEED_KEYS, "SeedProvider")
        state = self.current()
        provider_id = need_str(step["provider_id"], "provider_id")
        if provider_id in state["providers"]:
            raise RequestError(f"seed {provider_id} twice")
        if step["state"] not in PROVIDER_STATES:
            raise RequestError(f"seed state {step['state']}")
        uint64_max = int(sm.UINT64_MAX)
        capacity = need_int(step["admitted_capacity_bytes"], "capacity", 1, uint64_max)
        reserved = need_int(step["reserved_bytes"], "reserved", 0, capacity)
        state["providers"][provider_id] = {
            "account": need_str(step["account"], "account"),
            "signing_key_id": need_str(step["signing_key_id"], "signing_key_id"),
            "signing_pubkey": need_str(step["signing_pubkey"], "signing_pubkey"),
            "endpoint_manifest_digest": need_str(
                step["endpoint_manifest_digest"], "endpoint_manifest_digest"
            ),
            "key_generation": 1,
            "state": step["state"],
            "admitted_capacity_bytes": capacity,
            "reserved_bytes": reserved,
            "operator_group_id": need_str(step["operator_group_id"], "operator_group_id"),
            "previous_keys": [],
            "previous_pubkeys": [],
        }
        return self.respond("ok", False, [], None)


def handle(oracle: Oracle, request: object) -> JSON:
    try:
        req = need_dict(request, "request")
        cmd = req.get("cmd")
        if cmd == "reset":
            need_keys(req, frozenset({"cmd", "config", "disable_deviations"}), "reset")
            return oracle.reset(need_dict(req["config"], "config"), req["disable_deviations"])
        if cmd == "step":
            need_keys(req, frozenset({"cmd", "step"}), "step")
            return oracle.step(need_dict(req["step"], "step"))
        raise RequestError(f"unknown cmd {cmd!r}")
    except (RequestError, KeyError, TypeError) as exc:
        return {"error": f"{type(exc).__name__}: {exc}"}


def handle_line(oracle: Oracle, line: str) -> str:
    try:
        request = json.loads(line)
    except json.JSONDecodeError as exc:
        return json.dumps({"error": f"bad json: {exc}"})
    return json.dumps(handle(oracle, request), sort_keys=True, separators=(",", ":"))


def stream() -> int:
    oracle = Oracle()
    for line in sys.stdin:
        if not line.strip():
            continue
        sys.stdout.write(handle_line(oracle, line) + "\n")
        sys.stdout.flush()
    return 0


# Self-test: a fixed trace with its expected outputs.

T = sm.CHAIN_TIME
TERM = sm.TERM_SECONDS
P1_KEY = "key-p1"
# A second admitted service and a second provider account (not
# spec_machine.py accounts).
SERVICE2 = sm.addr(0x12)
PROVIDER2_ACCOUNT = sm.addr(0x34)


def self_config(max_expiry_steps: int) -> JSON:
    return {
        "chain_time": T,
        "term_seconds": TERM,
        "governed": {"max_expiry_steps": max_expiry_steps},
        "submitters": {
            sm.SERVICE: "ADMITTED_SERVICE",
            SERVICE2: "ADMITTED_SERVICE",
            sm.BROADCASTER: "BROADCASTER",
            sm.PROVIDER_ACCOUNT: "ADMITTED_PROVIDER",
            PROVIDER2_ACCOUNT: "ADMITTED_PROVIDER",
            sm.AUTHORITY: "AUTHORITY",
        },
        "max_offer_lifetime_seconds": 86400,
        "max_open_offers_per_content": 3,
        "max_open_offers_per_content_per_scheduler": 2,
    }


def offer_step(
    offer_id: str,
    content_id: str,
    deadline: int,
    expiry: int,
    business: str,
    signer: str = sm.SERVICE,
) -> JSON:
    step = sm.offer(offer_id, content_id=content_id, expiry=expiry)
    step["start_unix_seconds"] = deadline - TERM
    step["deadline_unix_seconds"] = deadline
    step["business_id"] = business
    step["signer"] = signer
    return step


def renew_step(lease_id: str, term_version: int, deadline: int, business: str) -> JSON:
    return {
        "op": "RenewLease",
        "signer": sm.BROADCASTER,
        "business_id": business,
        "lease_id": lease_id,
        "term_version": term_version,
        "deadline_unix_seconds": deadline,
        "package_bytes": sm.PACKAGE_BYTES,
        "manifest_digest": sm.MANIFEST,
        "provider_signature": sm.signature(sm.RENEW_DOMAIN, P1_KEY),
    }


def end_step(lease_id: str, business: str, term_version: int) -> JSON:
    signature = sm.signature(sm.END_DOMAIN, P1_KEY)
    signature["term_version"] = term_version
    return {
        "op": "EndLease",
        "signer": sm.BROADCASTER,
        "business_id": business,
        "lease_id": lease_id,
        "authority_end": False,
        "provider_signature": signature,
    }


def accept_step(offer_id: str, lease_id: str) -> JSON:
    step = sm.accept(offer_id, lease_id)
    del step["tx_hash"]
    return step


def setup_trace() -> list[Case]:
    trace: list[Case] = [
        (sm.register_provider(), "ok", None),
        ({"op": "CouncilAdmit", "provider_id": "p1"}, "ok", None),
    ]
    for content_id in ("c1", "c2", "c3", "c4"):
        trace.append((sm.register_content(content_id), "ok", None))
    return trace


def self_trace() -> list[Case]:
    """(step, expected code, expected deviation). max_expiry_steps is 1."""
    accept_l1 = accept_step("o1", "l1")
    return setup_trace() + [
        (offer_step("o1", "c1", T + TERM, T + 500, "b-o1"), "ok", None),
        (accept_l1, "ok", None),
        (accept_l1, "ok", None),  # the same business id and payload: idempotent
        (renew_step("l1", 2, T + 2 * TERM, "b-r1"), "ok", None),
        (renew_step("l1", 2, T + 2 * TERM, "b-r1b"), "ERROR_CODE_STALE_VERSION", None),
        # D-4: the accepted offer's id is never reused.
        (offer_step("o1", "c2", T + TERM, T + 500, "b-o1-again"), DUPLICATE_ID, "D-4"),
        # D-5: an offer lives at most max_offer_lifetime_seconds.
        (offer_step("o9", "c2", T + TERM, T + 86401, "b-o9"), BOUNDS_CODE, "D-5"),
        (offer_step("o8", "c2", T + TERM, T + 86400, "b-o8"), "ok", None),
        # D-10: one scheduler holds at most 2 (here) open offers of a content.
        (offer_step("o2", "c2", T + TERM, T + 500, "b-o2"), "ok", None),
        (offer_step("o4", "c2", T + TERM, T + 500, "b-o4"), BOUNDS_CODE, "D-10"),
        # D-6: at most max_open_offers_per_content (3 here) open offers.
        (offer_step("o3", "c2", T + TERM, T + 500, "b-o3", SERVICE2), "ok", None),
        (offer_step("o10", "c2", T + TERM, T + 500, "b-o10", SERVICE2), BOUNDS_CODE, "D-6"),
        # D-11: an identical replay by another scheduler is not idempotent.
        (offer_step("o3", "c2", T + TERM, T + 500, "b-o3"), FORBIDDEN, "D-11"),
        (offer_step("o3", "c2", T + TERM, T + 500, "b-o3", SERVICE2), "ok", None),
        # D-3: an offer whose deadline passed cannot be accepted.
        (offer_step("o5", "c3", T + 10, T + 100, "b-o5"), "ok", None),
        (offer_step("o6", "c4", T + 20, T + 100, "b-o6"), "ok", None),
        ({"op": "Block", "header_time": T + 10}, "ok", None),
        (accept_step("o5", "l5"), EXPIRED, "D-3"),
        (accept_step("o6", "l6"), "ok", None),
        (offer_step("o7", "c3", T + 20, T + 100, "b-o7"), "ok", None),
        (accept_step("o7", "l7"), "ok", None),
        # Two leases due at T+20, budget 1: l6 expires, l7 stays unswept.
        ({"op": "Block", "header_time": T + 20}, "ok", None),
        # D-2: a held lease past its deadline cannot renew, swept or not.
        (renew_step("l7", 2, T + 20 + TERM, "b-r7"), EXPIRED, "D-2"),
        (end_step("l6", "b-e6", 1), "ERROR_CODE_UNDERFLOW", None),
        # D-12: an end signed for term 1 does not end l1, renewed to term 2.
        (end_step("l1", "b-e1-stale", 1), UNAUTHORIZED, "D-12"),
        (end_step("l1", "b-e1", 2), "ok", None),
        (end_step("l1", "b-e1", 2), "ok", None),
        (end_step("l1", "b-e1b", 2), "ERROR_CODE_UNDERFLOW", None),
        ({"op": "Block", "header_time": T + 30}, "ok", None),
        # D-13: an acceptance replay goes to a BROADCASTER or the lease's
        # provider account, not to another provider's account.
        ({**accept_l1, "signer": PROVIDER2_ACCOUNT}, FORBIDDEN, "D-13"),
        ({**accept_l1, "signer": sm.PROVIDER_ACCOUNT}, "ok", None),
        (accept_l1, "ok", None),
    ]


def disabled_trace() -> list[Case]:
    """D-3 and D-2 cases that a run without the rules accepts (max_expiry_steps 1)."""
    return setup_trace() + [
        (offer_step("o5", "c3", T + 10, T + 100, "b-o5"), "ok", None),
        ({"op": "Block", "header_time": T + 10}, "ok", None),
        (accept_step("o5", "l5"), "ok", None),
        (offer_step("o6", "c4", T + 20, T + 100, "b-o6"), "ok", None),
        (accept_step("o6", "l6"), "ok", None),
        # Due at T+20: l5 (T+10) and l6 (T+20); budget 1 sweeps l5 only.
        ({"op": "Block", "header_time": T + 20}, "ok", None),
        (renew_step("l6", 2, T + 20 + TERM, "b-r6"), "ok", None),
    ]


def disabled_binding_trace() -> list[Case]:
    """D-12 and D-13 cases that a run without the rules accepts."""
    accept_l1 = accept_step("o1", "l1")
    return setup_trace() + [
        (offer_step("o1", "c1", T + TERM, T + 500, "b-o1"), "ok", None),
        (accept_l1, "ok", None),
        ({**accept_l1, "signer": PROVIDER2_ACCOUNT}, "ok", None),
        (renew_step("l1", 2, T + 2 * TERM, "b-r1"), "ok", None),
        (end_step("l1", "b-e1-stale", 1), "ok", None),
    ]


def run_trace(oracle: Oracle, trace: list[Case]) -> list[JSON]:
    problems: list[str] = []
    responses: list[JSON] = []
    for index, (step, code, deviation) in enumerate(trace):
        line = handle_line(oracle, json.dumps({"cmd": "step", "step": step}))
        response = json.loads(line)
        responses.append(response)
        if "error" in response:
            problems.append(f"step {index} {step['op']}: {response['error']}")
            continue
        if response["code"] != code or response["deviation"] != deviation:
            problems.append(
                f"step {index} {step['op']}: got {response['code']} {response['deviation']},"
                f" want {code} {deviation}"
            )
    if problems:
        raise AssertionError("; ".join(problems))
    return responses


def reset(oracle: Oracle, config: JSON, disable: bool) -> None:
    line = handle_line(
        oracle, json.dumps({"cmd": "reset", "config": config, "disable_deviations": disable})
    )
    if "error" in json.loads(line):
        raise AssertionError(line)


def self_test() -> int:
    oracle = Oracle()
    reset(oracle, self_config(1), disable=False)
    trace = self_trace()
    responses = run_trace(oracle, trace)
    final = responses[-1]["projection"]
    p1 = final["providers"][0]
    checks = {
        "idempotent accept": responses[8]["idempotent"] is True and responses[8]["events"] == [],
        "accept event": responses[7]["events"] == [{"type": "LeaseAccepted", "id": "l1"}],
        "l7 expired at T+30": [x["state"] for x in final["leases"] if x["lease_id"] == "l7"]
        == ["EXPIRED"],
        "reserved released": p1["reserved_bytes"] == "0",
        "pairs empty": final["pairs"] == [],
        "open offers": [x["offer_id"] for x in final["offers"]] == ["o2", "o3", "o5", "o8"],
        "idempotent offer replay": responses[19]["idempotent"] is True,
    }
    failed = [name for name, held in checks.items() if not held]
    if failed:
        raise AssertionError(f"self-test checks failed: {failed}")
    # With the rules disabled, every step a rule rejected is a spec success.
    oracle = Oracle()
    reset(oracle, self_config(1), disable=True)
    plain: list[Case] = [
        (step, "ok" if deviation is not None else code, None)
        for step, code, deviation in trace[:20]
    ]
    run_trace(oracle, plain)
    oracle = Oracle()
    reset(oracle, self_config(1), disable=True)
    run_trace(oracle, disabled_trace())
    oracle = Oracle()
    reset(oracle, self_config(1), disable=True)
    run_trace(oracle, disabled_binding_trace())
    # A bad request is an error response, not a code.
    bad = json.loads(handle_line(oracle, json.dumps({"cmd": "step", "step": {"op": "Nope"}})))
    if "error" not in bad:
        raise AssertionError("unknown op was not an error")
    steps = len(trace) + len(plain) + len(disabled_trace()) + len(disabled_binding_trace())
    print(f"spec_oracle self-test: PASS ({steps} steps)")
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--stream", action="store_true", help="JSON-lines protocol on stdin/stdout")
    mode.add_argument("--self-test", action="store_true", help="replay the built-in trace")
    args = parser.parse_args(argv)
    if args.self_test:
        return self_test()
    return stream()


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
