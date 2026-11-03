#!/usr/bin/env python3
"""Differential oracle for chain/x/leases and chain/x/evidence (keeper level, not end to end).

It drives docs/evidence/goal-02/spec_machine.py, the executable protocol v0
specification, one step at a time for the Go tests in chain/x/leases/difftest
(goal 08) and chain/x/evidence/difftest (goal 09). It lives under tests/
because the goal-01 contract checker allows Python only in the repository's
Python trees. It is test support. It does not run in chain execution and it
is not evidence of a committed transaction.

Run it as ``python -I spec_oracle.py --stream`` (the Go tests do) or
``python -I spec_oracle.py --self-test``. It uses only the standard library
and the two goal-02 modules (spec_machine.py, jcs.py). It reads no clock,
randomness, network, or file other than those modules and bounds.json. It
never calls jcs.py's text parser: requests are read with the standard json
module, and every request value the Go tests send is ASCII (the frozen
jcs.py parser accepts non-ASCII digits that the Go encoder refuses; that
divergence is outside this oracle's input domain).

Stream protocol: one JSON object per line on stdin, one JSON object per line
on stdout.

    {"cmd": "reset", "config": {...}, "disable_deviations": false}
    {"cmd": "step", "step": {...}}

reset config: chain_time, term_seconds (an integer, or null for a chain
without the DEC-005 devnet number), governed (governed limit values),
submitters (lowercase hex account -> role name),
max_offer_lifetime_seconds, max_open_offers_per_content, and
max_open_offers_per_content_per_scheduler (the chain constants of x/leases
deviations D-5, D-6, and D-10), and, only for the x/evidence tests,
"evidence" (below). Without "evidence" the oracle is exactly the goal-08
lease oracle: no evidence step, no evidence projection, no epoch change.

disable_deviations is false (every rule on), true (every rule off), or a
list of rule names to turn off (each rule is named in its deviation string).

A transaction step is a spec_machine step dict (OfferLease, AcceptLease,
RenewLease, EndLease, RegisterProvider, UpdateProvider, RotateProviderKey,
DrainProvider, RegisterContent, WithdrawContent), or, with "evidence", a
SubmitAuditBatch batch step (below). It goes to spec_machine.dispatch_tx on
a copy of the state, which is kept only when the step succeeds. A fixture
step is not a transaction: Block (CommitBlock, then ExpireDue with the
max_expiry_steps ceiling), CouncilAdmit, CouncilDisable,
CouncilForceWithdraw, GovernedLimit (apply_proposal_payload, kept only on
success), and SeedProvider (a direct provider record insert, like cases.py
seed_admitted).

An EndLease provider_signature may carry "term_version", the lease term
version the Go side's signature binds (chain/x/leases payload
SVN-PROVIDER-END-1). It is not a spec_machine.py field: the adapter always
removes it before the step reaches spec_machine.py.

The step response is {"code", "idempotent", "events", "deviation",
"projection"}. code is "ok" or an ERROR_CODE_* name. events lists the
svn.events.v1 events of the step as {"type", "id"}. deviation names the
chain deviation rules that changed the spec's outcome or that the adapter
applied, joined by "+", or null.

x/leases rules (chain/x/leases/README.md), named "D-n": D-2 to D-6 and D-10
apply only to a non-idempotent spec success, in the chain's order; D-11
applies only to an idempotent OfferLease success whose signer is not the
scheduler that committed the offer; D-13 only to an idempotent AcceptLease
success whose ADMITTED_PROVIDER signer is not the account of the lease's
provider. D-12 marks a provider EndLease signature that spec_machine.py's
check_signature would accept but that binds another term version than the
lease's current one as invalid, so the step is UNAUTHORIZED at the
signature check. Every rule discards the step.

x/evidence rules (chain/x/evidence/README.md), named "evidence D-n", only
with "evidence" in the reset config:

- evidence D-3: state["epoch"] is header_time // epoch_seconds, set at
  reset and at every Block (spec_machine.py keeps a static epoch 1). A Block
  that changes it reports the rule.
- evidence D-4: at a Block, state["evidence"] is swapped for {} while
  op_expire runs and restored after it, so evidence never uses the shared
  expiry budget and is never marked EXPIRED in the state (popping the key
  would make op_expire raise KeyError). A Block reports the rule when a
  RECORDED record falls due in it (previous chain_time < expiry <=
  header_time): spec_machine.py alone would sweep that record. The
  projection reports the derived state: RECORDED with expiry <= chain_time
  reads EXPIRED.
- evidence D-6 (a request mapping, never turned off): the chain carries N
  items under one signature; spec_machine.py takes one item and an abstract
  item_count. A one-item batch is one spec step with the item's fields. A
  batch of N != 1 items is one step with the first item's fields (for N = 0,
  the reset config's synthetic_item) and item_count = N, reported as
  "evidence D-6". Such a step must be rejected by the spec: the
  differential compares multi-item batches only in count classes.
- evidence D-17, D-8, D-9, D-10, D-11, D-12, D-18, D-13, D-15, in that
  (the chain's tail) order, on a non-idempotent one-item spec success: id
  without the signer's prefix FORBIDDEN; PROVIDER_RECEIPT FORBIDDEN;
  expiry - chain_time > max_evidence_lifetime_seconds BOUNDS; content id or
  version differs from the lease CONFLICT; lease not held CONFLICT; lease
  deadline <= chain_time EXPIRED; expiry > lease deadline CONFLICT; lease
  content withdrawn WITHDRAWN; the signer's accepted items in the current
  epoch (chain_time // epoch_seconds) reach max_evidence_per_auditor_per_epoch
  BOUNDS. The adapter counts the items it accepted per signer and epoch.

No other semantics live in this adapter. A request it cannot read returns
{"error": "..."}.

"evidence" config: epoch_seconds, max_evidence_lifetime_seconds,
max_evidence_per_auditor_per_epoch, id_prefix_bytes (the chain constants
of the rules above), and synthetic_item (the item of an empty batch).

SubmitAuditBatch step: {"op", "signer", "business_id", "batch_id", "epoch",
"auditor_signature", "items"}; each item has evidence_id, kind, lease_id,
content_id, content_version, expiry_unix_seconds, attestation, and
observation_digest. auditor_signature is the spec's abstract signature
({domain, key_id, valid, observation_digest}); its validity is fixed by the
Go generator, never by this adapter.

With "evidence", the projection also carries "evidence" (per evidence id:
batch_id, kind, derived state, stored_state, lease_id, content_id,
expiry_unix_seconds, observation_digest, attestation) and "batch_ids".
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
AUDIT_OP: Final = "SubmitAuditBatch"
ROLES: Final = frozenset(
    {"ADMITTED_SERVICE", "BROADCASTER", "ADMITTED_PROVIDER", "AUDITOR", "AUTHORITY"}
)
PROVIDER_STATES: Final = frozenset({"APPLIED", "ADMITTED", "DRAINING", "DISABLED"})
# The id an event is reported by: the first of these fields it carries.
EVENT_ID_FIELDS: Final = ("lease_id", "offer_id", "provider_id", "content_id", "evidence_id")
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
EVIDENCE_CONFIG_KEYS: Final = frozenset(
    {
        "epoch_seconds",
        "max_evidence_lifetime_seconds",
        "max_evidence_per_auditor_per_epoch",
        "id_prefix_bytes",
        "synthetic_item",
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
BATCH_KEYS: Final = frozenset(
    {"op", "signer", "business_id", "batch_id", "epoch", "auditor_signature", "items"}
)
ITEM_KEYS: Final = frozenset(
    {
        "evidence_id",
        "kind",
        "lease_id",
        "content_id",
        "content_version",
        "expiry_unix_seconds",
        "attestation",
        "observation_digest",
    }
)
LEASE_RULES: Final = frozenset({"D-2", "D-3", "D-4", "D-5", "D-6", "D-10", "D-11", "D-12", "D-13"})
EVIDENCE_RULES: Final = frozenset(
    {
        "evidence D-3",
        "evidence D-4",
        "evidence D-8",
        "evidence D-9",
        "evidence D-10",
        "evidence D-11",
        "evidence D-12",
        "evidence D-13",
        "evidence D-15",
        "evidence D-17",
        "evidence D-18",
    }
)
BATCH_MAPPING: Final = "evidence D-6"
HELD_STATES: Final = frozenset({"ACTIVE", "RENEWED"})
DUPLICATE_ID: Final = "ERROR_CODE_DUPLICATE_ID"
BOUNDS_CODE: Final = "ERROR_CODE_BOUNDS"
FORBIDDEN: Final = "ERROR_CODE_FORBIDDEN"
EXPIRED: Final = "ERROR_CODE_EXPIRED"
UNAUTHORIZED: Final = "ERROR_CODE_UNAUTHORIZED"
CONFLICT: Final = "ERROR_CODE_CONFLICT"
WITHDRAWN: Final = "ERROR_CODE_WITHDRAWN"
NOT_FOUND: Final = "ERROR_CODE_NOT_FOUND"


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


def need_item(value: object, name: str) -> JSON:
    item = need_dict(value, name)
    need_keys(item, ITEM_KEYS, name)
    return item


def event_ref(event: JSON) -> JSON:
    for field in EVENT_ID_FIELDS:
        value = event.get(field)
        if isinstance(value, str) and value:
            return {"type": str(event["type"]), "id": value}
    raise RequestError(f"event {event.get('type')} carries no id field")


def derived_state(record: JSON, chain_time: int) -> str:
    """Evidence D-4: a RECORDED record reads EXPIRED from its expiry on."""
    stored = str(record["state"])
    if stored == "RECORDED" and record["expiry_unix_seconds"] <= chain_time:
        return "EXPIRED"
    return stored


def projection(state: JSON, evidence: bool) -> JSON:
    """The chain-state projection both sides compare.

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
    out: JSON = {
        "chain_time": str(state["chain_time"]),
        "providers": providers,
        "contents": contents,
        "offers": offers,
        "leases": leases,
        "pairs": pairs,
        "business_ids": sorted(state["business"]),
    }
    if evidence:
        records = []
        for evidence_id in sorted(state["evidence"]):
            r = state["evidence"][evidence_id]
            records.append(
                {
                    "evidence_id": evidence_id,
                    "batch_id": r["batch_id"],
                    "kind": r["kind"],
                    "state": derived_state(r, state["chain_time"]),
                    "stored_state": r["state"],
                    "lease_id": r["lease_id"],
                    "content_id": r["content_id"],
                    "expiry_unix_seconds": str(r["expiry_unix_seconds"]),
                    "observation_digest": r["observation_digest"],
                    "attestation": bool(r["attestation"]),
                }
            )
        out["evidence"] = records
        out["batch_ids"] = sorted(state["batches"])
    return out


class EvidenceConfig:
    """The x/evidence chain constants of the evidence rules."""

    def __init__(self, config: JSON) -> None:
        need_keys(config, EVIDENCE_CONFIG_KEYS, "evidence config")
        self.epoch_seconds = need_int(config["epoch_seconds"], "epoch_seconds", 1, sm.SAFE_MAX)
        self.lifetime = need_int(
            config["max_evidence_lifetime_seconds"], "max_evidence_lifetime_seconds", 1, sm.SAFE_MAX
        )
        self.per_epoch = need_int(
            config["max_evidence_per_auditor_per_epoch"],
            "max_evidence_per_auditor_per_epoch",
            1,
            sm.SAFE_MAX,
        )
        self.prefix_bytes = need_int(
            config["id_prefix_bytes"], "id_prefix_bytes", 1, int(sm.BOUNDS["account_address_bytes"])
        )
        self.synthetic_item = need_item(config["synthetic_item"], "synthetic_item")

    def epoch(self, chain_time: int) -> int:
        return chain_time // self.epoch_seconds

    def prefix(self, signer: str) -> str:
        return signer[: 2 * self.prefix_bytes] + "."


class Oracle:
    """spec_machine.py state plus the x/leases and x/evidence deviation rules."""

    def __init__(self) -> None:
        self.state: JSON | None = None
        # Every offer id an OfferLease committed, with its scheduler (the
        # chain's used-offer marker).
        self.used_offer_ids: dict[str, str] = {}
        self.disabled: frozenset[str] = frozenset()
        self.max_offer_lifetime = 0
        self.max_open_offers_per_content = 0
        self.max_open_offers_per_content_per_scheduler = 0
        self.evidence: EvidenceConfig | None = None
        # signer -> (epoch, items accepted in that epoch): the chain's
        # auditor window (evidence D-15).
        self.windows: dict[str, tuple[int, int]] = {}

    def on(self, rule: str) -> bool:
        return rule not in self.disabled

    def reset(self, config: JSON, disable_deviations: object) -> JSON:
        evidence_config = config.get("evidence")
        need_keys(
            config,
            CONFIG_KEYS | ({"evidence"} if evidence_config is not None else frozenset()),
            "config",
        )
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
        self.evidence = (
            None
            if evidence_config is None
            else EvidenceConfig(need_dict(evidence_config, "evidence"))
        )
        self.disabled = self.read_disabled(disable_deviations)
        if self.evidence is not None and self.on("evidence D-3"):
            state["epoch"] = self.evidence.epoch(state["chain_time"])
        self.used_offer_ids = {}
        self.windows = {}
        self.state = state
        return {"ok": True, "projection": projection(state, self.evidence is not None)}

    def read_disabled(self, value: object) -> frozenset[str]:
        known = LEASE_RULES | (EVIDENCE_RULES if self.evidence is not None else frozenset())
        if value is False:
            return frozenset()
        if value is True:
            return known
        if not isinstance(value, list) or not all(isinstance(rule, str) for rule in value):
            raise RequestError("disable_deviations must be a boolean or a list of rule names")
        unknown_rules = set(value) - known
        if unknown_rules:
            raise RequestError(f"unknown deviation rules {sorted(unknown_rules)}")
        return frozenset(value)

    def current(self) -> JSON:
        if self.state is None:
            raise RequestError("step before reset")
        return self.state

    def step(self, step: JSON) -> JSON:
        op = step.get("op")
        if op in TX_OPS:
            return self.transaction(step)
        if op == AUDIT_OP:
            return self.audit(step)
        if op == "Block":
            return self.block(step)
        if op in ("CouncilAdmit", "CouncilDisable", "CouncilForceWithdraw", "GovernedLimit"):
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
            "projection": projection(self.current(), self.evidence is not None),
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
        if ok and not idempotent:
            rule = self.deviation(pre, step)
            if rule is not None:
                deviation, code = rule
        if ok and idempotent and self.on("D-11") and step["op"] == "OfferLease":
            # D-11: the idempotent success goes only to the offer's scheduler.
            if self.used_offer_ids.get(step["offer_id"]) != step["signer"]:
                return self.respond(FORBIDDEN, False, [], "D-11")
        if ok and idempotent and self.on("D-13") and step["op"] == "AcceptLease":
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
        whether the rule marked the signature invalid: rule on, a provider
        end of an existing lease, a signature check_signature would accept,
        and a term version other than the lease's current one.
        """
        sig = step.get("provider_signature")
        if step.get("op") != "EndLease" or not isinstance(sig, dict) or "term_version" not in sig:
            return step, False
        signed = need_int(sig["term_version"], "term_version", 1, sm.SAFE_MAX)
        plain = {name: value for name, value in sig.items() if name != "term_version"}
        step = {**step, "provider_signature": plain}
        if not self.on("D-12") or step.get("authority_end") is not False:
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
        """The x/leases chain-only checks after every spec check (D-2..D-6, D-10)."""
        now = pre["chain_time"]
        op = step["op"]
        if op == "OfferLease":
            if self.on("D-4") and step["offer_id"] in self.used_offer_ids:
                return "D-4", DUPLICATE_ID
            if self.on("D-5") and step["expiry_unix_seconds"] - now > self.max_offer_lifetime:
                return "D-5", BOUNDS_CODE
            content_id = step["content_id"]
            open_offers = sum(1 for o in pre["offers"].values() if o["content_id"] == content_id)
            if self.on("D-6") and open_offers >= self.max_open_offers_per_content:
                return "D-6", BOUNDS_CODE
            own = sum(
                1
                for o in pre["offers"].values()
                if o["content_id"] == content_id and o["scheduler"] == step["signer"]
            )
            if self.on("D-10") and own >= self.max_open_offers_per_content_per_scheduler:
                return "D-10", BOUNDS_CODE
        elif op == "AcceptLease":
            if self.on("D-3") and pre["offers"][step["offer_id"]]["deadline_unix_seconds"] <= now:
                return "D-3", EXPIRED
        elif op == "RenewLease":
            if self.on("D-2") and pre["leases"][step["lease_id"]]["deadline_unix_seconds"] <= now:
                return "D-2", EXPIRED
        return None

    def evidence_config(self) -> EvidenceConfig:
        if self.evidence is None:
            raise RequestError("SubmitAuditBatch needs the evidence reset config")
        return self.evidence

    def audit_step(self, step: JSON) -> tuple[JSON, int]:
        """Evidence D-6: the spec step of a batch, and its item count."""
        cfg = self.evidence_config()
        need_keys(step, BATCH_KEYS, AUDIT_OP)
        items = step["items"]
        if not isinstance(items, list):
            raise RequestError("items must be a list")
        for index, item in enumerate(items):
            need_item(item, f"item {index}")
        spec: JSON = {
            name: step[name]
            for name in ("op", "signer", "business_id", "batch_id", "epoch", "auditor_signature")
        }
        spec.update(items[0] if items else cfg.synthetic_item)
        if len(items) != 1:
            spec["item_count"] = len(items)
        return spec, len(items)

    def audit(self, step: JSON) -> JSON:
        pre = self.current()
        before = len(pre["events"])
        spec, count = self.audit_step(step)
        trial = copy.deepcopy(pre)
        result = sm.dispatch_tx(trial, spec)
        ok = bool(result["ok"])
        idempotent = bool(result["idempotent"])
        code = "ok" if ok else str(result["error"])
        mapping = BATCH_MAPPING if count != 1 else None
        if ok and not idempotent and count != 1:
            raise RequestError(
                f"a {count}-item batch succeeded in the spec; only count classes compare"
            )
        if ok and not idempotent:
            rule = self.audit_tail(pre, spec)
            if rule is not None:
                return self.respond(rule[1], False, [], rule[0])
        if not result["keep"]:
            return self.respond(code, False, [], mapping)
        self.state = trial
        if ok and not idempotent:
            cfg = self.evidence_config()
            epoch = cfg.epoch(pre["chain_time"])
            window = self.windows.get(spec["signer"])
            held = window[1] if window is not None and window[0] == epoch else 0
            self.windows[spec["signer"]] = (epoch, held + 1)
        return self.respond(code, idempotent, trial["events"][before:], mapping)

    def audit_tail(self, pre: JSON, step: JSON) -> tuple[str, str] | None:
        """The x/evidence chain-only checks T1-T9 after every spec check."""
        cfg = self.evidence_config()
        now = pre["chain_time"]
        prefix = cfg.prefix(step["signer"])
        if self.on("evidence D-17") and not (
            step["batch_id"].startswith(prefix) and step["evidence_id"].startswith(prefix)
        ):
            return "evidence D-17", FORBIDDEN
        if self.on("evidence D-8") and step["kind"] == "PROVIDER_RECEIPT":
            return "evidence D-8", FORBIDDEN
        if self.on("evidence D-9") and step["expiry_unix_seconds"] - now > cfg.lifetime:
            return "evidence D-9", BOUNDS_CODE
        lease = pre["leases"][step["lease_id"]]
        if self.on("evidence D-10") and (
            step["content_id"] != lease["content_id"]
            or step["content_version"] != lease["content_version"]
        ):
            return "evidence D-10", CONFLICT
        if self.on("evidence D-11") and not (
            lease["reservation_held"] and lease["state"] in HELD_STATES
        ):
            return "evidence D-11", CONFLICT
        if self.on("evidence D-12") and lease["deadline_unix_seconds"] <= now:
            return "evidence D-12", EXPIRED
        if (
            self.on("evidence D-18")
            and step["expiry_unix_seconds"] > lease["deadline_unix_seconds"]
        ):
            return "evidence D-18", CONFLICT
        if self.on("evidence D-13"):
            content = pre["contents"].get(lease["content_id"])
            if content is None:
                return "evidence D-13", NOT_FOUND
            if content["state"] == "WITHDRAWN":
                return "evidence D-13", WITHDRAWN
        if self.on("evidence D-15"):
            window = self.windows.get(step["signer"])
            held = window[1] if window is not None and window[0] == cfg.epoch(now) else 0
            if held + 1 > cfg.per_epoch:
                return "evidence D-15", BOUNDS_CODE
        return None

    @staticmethod
    def newly_due_evidence(state: JSON, previous: int, header_time: int) -> bool:
        """Whether a RECORDED record falls due in (previous, header_time]."""
        return any(
            record["state"] == "RECORDED"
            and previous < record["expiry_unix_seconds"] <= header_time
            for record in state["evidence"].values()
        )

    def block(self, step: JSON) -> JSON:
        need_keys(step, frozenset({"op", "header_time"}), "Block")
        header_time = need_int(step["header_time"], "header_time", 1, sm.SAFE_MAX)
        pre = self.current()
        before = len(pre["events"])
        trial = copy.deepcopy(pre)
        rules: list[str] = []
        result = sm.dispatch(trial, {"op": "CommitBlock", "header_time": header_time})
        if result["ok"]:
            if self.evidence is not None and self.on("evidence D-3"):
                epoch = self.evidence.epoch(header_time)
                if trial["epoch"] != epoch:
                    rules.append("evidence D-3")
                trial["epoch"] = epoch
            ceiling = int(sm.BOUNDS["governed_limit_caps"]["max_expiry_steps"])
            expire = {"op": "ExpireDue", "limit": ceiling}
            if self.evidence is not None and self.on("evidence D-4"):
                if self.newly_due_evidence(trial, pre["chain_time"], header_time):
                    rules.append("evidence D-4")
                saved = trial["evidence"]
                trial["evidence"] = {}
                result = sm.dispatch(trial, expire)
                trial["evidence"] = saved
            else:
                result = sm.dispatch(trial, expire)
        if not result["ok"]:
            return self.respond(str(result["error"]), False, [], None)
        self.state = trial
        return self.respond("ok", False, trial["events"][before:], "+".join(rules) or None)

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
        elif op == "GovernedLimit":
            need_keys(step, frozenset({"op", "limit_key", "value"}), op)
            key = need_str(step["limit_key"], "limit_key")
            if key not in sm.BOUNDS["governed_limit_caps"]:
                raise RequestError(f"unknown governed limit {key}")
            payload = {
                "kind": "governed_limit",
                "limit_key": key,
                "value": need_int(step["value"], key, 1, sm.SAFE_MAX),
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


# Self-test: fixed traces with their expected outputs.

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


def renew_step(
    lease_id: str, term_version: int, deadline: int, business: str, evidence_id: str = ""
) -> JSON:
    step = {
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
    if evidence_id:
        step["evidence_id"] = evidence_id
    return step


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


# Evidence self-test. EPOCH is 3600 s; T0 is the first epoch boundary after
# sm.CHAIN_TIME. An id prefix is the first 8 address bytes in hex and ".".
EPOCH: Final = 3600
T0: Final = (T // EPOCH + 1) * EPOCH
NOW: Final = T0 + 100
DUE: Final = NOW + TERM
AUDITOR2: Final = sm.addr(0x45)
PREFIX: Final = sm.AUDITOR[:16] + "."
PREFIX2: Final = AUDITOR2[:16] + "."
OTHER_DIGEST: Final = "ab" * 32
SYNTHETIC_ITEM: Final[JSON] = {
    "evidence_id": "synthetic-item",
    "kind": "AUDITOR_ATTESTATION",
    "lease_id": "synthetic-lease",
    "content_id": "synthetic-content",
    "content_version": 1,
    "expiry_unix_seconds": 1,
    "attestation": True,
    "observation_digest": "00" * 32,
}


def evidence_config(max_expiry_steps: int, per_epoch: int = 1024) -> JSON:
    config = self_config(max_expiry_steps)
    config["chain_time"] = NOW
    config["submitters"][sm.AUDITOR] = "AUDITOR"
    config["submitters"][AUDITOR2] = "AUDITOR"
    config["evidence"] = {
        "epoch_seconds": EPOCH,
        "max_evidence_lifetime_seconds": 86400,
        "max_evidence_per_auditor_per_epoch": per_epoch,
        "id_prefix_bytes": 8,
        "synthetic_item": SYNTHETIC_ITEM,
    }
    return config


def item(
    evidence_id: str,
    lease_id: str,
    expiry: int,
    digest: str = sm.MANIFEST,
    content_id: str = "c1",
    content_version: int = 1,
    kind: str = "AUDITOR_ATTESTATION",
) -> JSON:
    return {
        "evidence_id": evidence_id,
        "kind": kind,
        "lease_id": lease_id,
        "content_id": content_id,
        "content_version": content_version,
        "expiry_unix_seconds": expiry,
        "attestation": True,
        "observation_digest": digest,
    }


def audit(
    business: str,
    batch_id: str,
    items: list[JSON],
    at: int,
    signer: str = sm.AUDITOR,
    valid: bool = True,
) -> JSON:
    """A batch step for the epoch of time at."""
    first = items[0] if items else SYNTHETIC_ITEM
    return {
        "op": AUDIT_OP,
        "signer": signer,
        "business_id": business,
        "batch_id": batch_id,
        "epoch": at // EPOCH,
        "auditor_signature": sm.signature(
            sm.EVIDENCE_DOMAIN,
            sm.AUDITOR_KEY,
            valid=valid,
            observation_digest=first["observation_digest"],
        ),
        "items": items,
    }


def withdraw_step(content_id: str, business: str) -> JSON:
    return {
        "op": "WithdrawContent",
        "signer": sm.SERVICE,
        "business_id": business,
        "content_id": content_id,
        "content_version": 1,
        "hosting_intent": sm.intent(),
        "reason_digest": sm.REASON,
    }


def evidence_setup() -> list[Case]:
    """p1 admitted; c1-c4; leases l1 (c1) and l2 (c2) due at DUE; l3 (c3) ended."""
    trace = setup_trace()
    for index in (1, 2, 3):
        trace.append(
            (offer_step(f"o{index}", f"c{index}", DUE, NOW + 500, f"b-o{index}"), "ok", None)
        )
        trace.append((accept_step(f"o{index}", f"l{index}"), "ok", None))
    trace.append((end_step("l3", "b-e3", 1), "ok", None))
    return trace


def evidence_trace() -> list[Case]:
    """Every evidence rule once, the renewal reads, and the epoch (max_expiry_steps 1)."""
    later = T0 + EPOCH
    return evidence_setup() + [
        (audit("b-ev1", PREFIX + "b1", [item(PREFIX + "e1", "l1", NOW + 600)], NOW), "ok", None),
        # The same business id and payload: idempotent; another payload: REPLAY.
        (audit("b-ev1", PREFIX + "b1", [item(PREFIX + "e1", "l1", NOW + 600)], NOW), "ok", None),
        (
            audit("b-ev1", PREFIX + "b1x", [item(PREFIX + "e1", "l1", NOW + 600)], NOW),
            "ERROR_CODE_REPLAY",
            None,
        ),
        (
            audit("b-ev2", PREFIX + "b1", [item(PREFIX + "e2", "l1", NOW + 600)], NOW),
            DUPLICATE_ID,
            None,
        ),
        (
            audit(
                "b-ev3", PREFIX + "b3", [item(PREFIX + "e3", "l1", NOW + 600, OTHER_DIGEST)], NOW
            ),
            "ok",
            None,
        ),
        (
            audit("b-ev4", PREFIX + "b4", [item(PREFIX + "e4", "l1", NOW + 600)], NOW, valid=False),
            UNAUTHORIZED,
            None,
        ),
        (
            audit("b-ev5", PREFIX + "b5", [item(PREFIX + "e5", "l-none", NOW + 600)], NOW),
            NOT_FOUND,
            None,
        ),
        # Evidence D-6: an empty or oversized batch is one step with item_count.
        (audit("b-ev6", PREFIX + "b6", [], NOW), BOUNDS_CODE, BATCH_MAPPING),
        (
            audit(
                "b-ev7",
                PREFIX + "b7",
                [item(PREFIX + f"m{i}", "l1", NOW + 600) for i in range(33)],
                NOW,
            ),
            BOUNDS_CODE,
            BATCH_MAPPING,
        ),
        # The tail rules, in the chain's order.
        (
            audit("b-ev8", PREFIX2 + "b8", [item(PREFIX2 + "e8", "l1", NOW + 600)], NOW),
            FORBIDDEN,
            "evidence D-17",
        ),
        (
            audit(
                "b-ev9",
                PREFIX + "b9",
                [item(PREFIX + "e9", "l1", NOW + 600, kind="PROVIDER_RECEIPT")],
                NOW,
            ),
            FORBIDDEN,
            "evidence D-8",
        ),
        (
            audit("b-ev10", PREFIX + "b10", [item(PREFIX + "e10", "l1", NOW + 86401)], NOW),
            BOUNDS_CODE,
            "evidence D-9",
        ),
        (
            audit(
                "b-ev11",
                PREFIX + "b11",
                [item(PREFIX + "e11", "l1", NOW + 600, content_version=2)],
                NOW,
            ),
            CONFLICT,
            "evidence D-10",
        ),
        (
            audit(
                "b-ev12",
                PREFIX + "b12",
                [item(PREFIX + "e12", "l3", NOW + 600, content_id="c3")],
                NOW,
            ),
            CONFLICT,
            "evidence D-11",
        ),
        (
            audit("b-ev13", PREFIX + "b13", [item(PREFIX + "e13", "l1", DUE + 1)], NOW),
            CONFLICT,
            "evidence D-18",
        ),
        (
            audit(
                "b-ev14",
                PREFIX2 + "b14",
                [item(PREFIX2 + "e14", "l2", NOW + 1, content_id="c2")],
                NOW,
                AUDITOR2,
            ),
            "ok",
            None,
        ),
        # No evidence state renews a lease.
        (renew_step("l1", 2, DUE + TERM, "b-r-live", PREFIX + "e1"), FORBIDDEN, None),
        (renew_step("l1", 2, DUE + TERM, "b-r-rej", PREFIX + "e3"), FORBIDDEN, None),
        (renew_step("l1", 2, DUE + TERM, "b-r-none", PREFIX + "zz"), NOT_FOUND, None),
        # Evidence D-4: e14 falls due; nothing is swept and it reads EXPIRED.
        ({"op": "Block", "header_time": NOW + 1}, "ok", "evidence D-4"),
        (renew_step("l2", 2, DUE + TERM, "b-r-exp", PREFIX2 + "e14"), EXPIRED, None),
        # Evidence D-3: the epoch follows the header time.
        ({"op": "Block", "header_time": later}, "ok", "evidence D-3+evidence D-4"),
        (
            audit("b-ev15", PREFIX + "b15", [item(PREFIX + "e15", "l1", later + 60)], NOW),
            "ERROR_CODE_STALE_VERSION",
            None,
        ),
        (
            audit("b-ev15", PREFIX + "b15", [item(PREFIX + "e15", "l1", later + 60)], later),
            "ok",
            None,
        ),
        # Evidence D-13: withdrawn content keeps its leases held.
        (withdraw_step("c2", "b-w2"), "ok", None),
        (
            audit(
                "b-ev16",
                PREFIX + "b16",
                [item(PREFIX + "e16", "l2", later + 60, content_id="c2")],
                later,
            ),
            WITHDRAWN,
            "evidence D-13",
        ),
        # Evidence D-12: l1 and l2 fall due together; a budget of 1 sweeps
        # l1, and l2 stays held past its deadline.
        ({"op": "Block", "header_time": DUE}, "ok", "evidence D-4"),
        (
            audit(
                "b-ev17",
                PREFIX + "b17",
                [item(PREFIX + "e17", "l2", DUE + 60, content_id="c2")],
                DUE,
            ),
            EXPIRED,
            "evidence D-12",
        ),
    ]


def window_trace() -> list[Case]:
    """Evidence D-15 with a window of 2 items per auditor and epoch."""
    later = T0 + EPOCH
    return evidence_setup() + [
        (audit("b-w1", PREFIX + "w1", [item(PREFIX + "w1", "l1", NOW + 600)], NOW), "ok", None),
        (audit("b-w2", PREFIX + "w2", [item(PREFIX + "w2", "l1", NOW + 600)], NOW), "ok", None),
        (
            audit("b-w3", PREFIX + "w3", [item(PREFIX + "w3", "l1", NOW + 600)], NOW),
            BOUNDS_CODE,
            "evidence D-15",
        ),
        (
            audit("b-w4", PREFIX2 + "w4", [item(PREFIX2 + "w4", "l1", NOW + 600)], NOW, AUDITOR2),
            "ok",
            None,
        ),
        ({"op": "Block", "header_time": later}, "ok", "evidence D-3+evidence D-4"),
        (audit("b-w5", PREFIX + "w5", [item(PREFIX + "w5", "l1", later + 60)], later), "ok", None),
    ]


def without_evidence_rules(trace: list[Case]) -> list[Case]:
    """The trace with every evidence rule but D-3 off: each rule step is ok."""
    out: list[Case] = []
    for step, code, deviation in trace:
        if deviation is None or deviation == BATCH_MAPPING:
            out.append((step, code, deviation))
        elif step["op"] == "Block":
            kept = [rule for rule in deviation.split("+") if rule == "evidence D-3"]
            out.append((step, code, "+".join(kept) or None))
        else:
            out.append((step, "ok", None))
    return out


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


def reset(oracle: Oracle, config: JSON, disable: object) -> None:
    line = handle_line(
        oracle, json.dumps({"cmd": "reset", "config": config, "disable_deviations": disable})
    )
    if "error" in json.loads(line):
        raise AssertionError(line)


def lease_self_test() -> int:
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
        "no evidence projection": "evidence" not in final and "batch_ids" not in final,
    }
    failed = [name for name, held in checks.items() if not held]
    if failed:
        raise AssertionError(f"lease self-test checks failed: {failed}")
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
    # An evidence step needs the evidence config.
    refused = json.loads(handle_line(oracle, json.dumps({"cmd": "step", "step": {"op": AUDIT_OP}})))
    if "error" not in refused:
        raise AssertionError("an evidence step without the evidence config was not an error")
    return len(trace) + len(plain) + len(disabled_trace()) + len(disabled_binding_trace())


def evidence_self_test() -> int:
    oracle = Oracle()
    reset(oracle, evidence_config(1), disable=False)
    trace = evidence_trace()
    responses = run_trace(oracle, trace)
    base = len(evidence_setup())
    final = responses[-1]["projection"]
    records = {r["evidence_id"]: r for r in final["evidence"]}
    checks = {
        "accepted event": responses[base]["events"]
        == [{"type": "EvidenceAccepted", "id": PREFIX + "e1"}],
        "idempotent audit": responses[base + 1]["idempotent"] is True
        and responses[base + 1]["events"] == [],
        "records": sorted(records)
        == sorted([PREFIX + "e1", PREFIX + "e3", PREFIX + "e15", PREFIX2 + "e14"]),
        "rejected stays rejected": records[PREFIX + "e3"]["state"] == "REJECTED",
        "expired reads expired": records[PREFIX + "e1"]["state"] == "EXPIRED"
        and records[PREFIX + "e1"]["stored_state"] == "RECORDED",
        "batches": final["batch_ids"]
        == sorted([PREFIX + "b1", PREFIX + "b15", PREFIX + "b3", PREFIX2 + "b14"]),
        "l2 held past its deadline": [
            x["reservation_held"] for x in final["leases"] if x["lease_id"] == "l2"
        ]
        == [True],
        "l1 swept": [x["state"] for x in final["leases"] if x["lease_id"] == "l1"] == ["EXPIRED"],
    }
    failed = [name for name, held in checks.items() if not held]
    if failed:
        raise AssertionError(f"evidence self-test checks failed: {failed}")
    # With every evidence rule but D-3 off, each rule step is a spec success.
    keep_epoch = sorted(EVIDENCE_RULES - {"evidence D-3"})
    oracle = Oracle()
    reset(oracle, evidence_config(1), disable=keep_epoch)
    plain = without_evidence_rules(trace)
    run_trace(oracle, plain)
    # Without D-3 the spec's epoch stays 1, so every audit is STALE_VERSION.
    oracle = Oracle()
    reset(oracle, evidence_config(1), disable=["evidence D-3"])
    first = evidence_setup() + [trace[base]]
    stale = first[:-1] + [(first[-1][0], "ERROR_CODE_STALE_VERSION", None)]
    run_trace(oracle, stale)
    # Evidence D-15 with a window of 2.
    oracle = Oracle()
    reset(oracle, evidence_config(1, per_epoch=2), disable=False)
    window = window_trace()
    run_trace(oracle, window)
    oracle = Oracle()
    reset(oracle, evidence_config(1, per_epoch=2), disable=keep_epoch)
    run_trace(oracle, without_evidence_rules(window))
    # A multi-item batch that the spec accepts is a harness error.
    oracle = Oracle()
    reset(oracle, evidence_config(1), disable=False)
    run_trace(oracle, evidence_setup())
    two = audit(
        "b-two",
        PREFIX + "two",
        [item(PREFIX + "t1", "l1", NOW + 600), item(PREFIX + "t2", "l1", NOW + 600)],
        NOW,
    )
    if "error" not in json.loads(handle_line(oracle, json.dumps({"cmd": "step", "step": two}))):
        raise AssertionError("an accepted 2-item batch was not an error")
    # Unknown rule names are refused.
    bad = handle_line(
        Oracle(),
        json.dumps({"cmd": "reset", "config": evidence_config(1), "disable_deviations": ["D-99"]}),
    )
    if "error" not in json.loads(bad):
        raise AssertionError("an unknown rule name was accepted")
    return len(trace) + len(plain) + len(stale) + 2 * len(window) + len(evidence_setup())


def self_test() -> int:
    steps = lease_self_test() + evidence_self_test()
    # A bad request is an error response, not a code.
    oracle = Oracle()
    reset(oracle, self_config(1), disable=False)
    bad = json.loads(handle_line(oracle, json.dumps({"cmd": "step", "step": {"op": "Nope"}})))
    if "error" not in bad:
        raise AssertionError("unknown op was not an error")
    print(f"spec_oracle self-test: PASS ({steps} steps)")
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--stream", action="store_true", help="JSON-lines protocol on stdin/stdout")
    mode.add_argument("--self-test", action="store_true", help="replay the built-in traces")
    args = parser.parse_args(argv)
    if args.self_test:
        return self_test()
    return stream()


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
