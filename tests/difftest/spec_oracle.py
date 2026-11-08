#!/usr/bin/env python3
"""Differential oracle for chain/x/leases, chain/x/evidence and chain/x/authority (keeper level).

It drives docs/evidence/goal-02/spec_machine.py, the executable protocol v0
specification, one step at a time for the Go tests in chain/x/leases/difftest
(goal 08), chain/x/evidence/difftest (goal 09) and chain/x/authority/difftest
(goal 10, council flow). It is not end-to-end evidence. It lives under tests/
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
    {"cmd": "step", "step": {...}, "projection": false}

A step request with "projection": false is answered with "projection":
null; nothing else changes (the Go tests use it for steps where they compare
only the code, idempotent flag, events, and deviation).

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

Council flow (goal 10), only with "council" in the reset config. Without it
a council step is a request error and nothing above changes. "council"
config: seats (a list of {address, seat_id, pubkey}: the 20-byte account in
hex, the seat id, the 32-byte council key in hex), threshold and
delay_seconds (the DEC-008 devnet numeric value and delay), height (the
current block height), validators (a list of {node_id, power,
consensus_pubkey}), and the x/authority chain constants approved_seats,
approved_threshold, max_open_proposals_per_seat and max_pending_changes.

A council step is ProposeChange, ApproveChange or ExecuteChange in the
spec_machine.py field names. The signer is a 20-byte account in hex; for a
propose or an approve the adapter replaces a seat account with its seat id
(spec seats are names). A propose payload is proto3 JSON and payload_digest
is hex. A propose may carry "digest_algorithm", the payload digest's
algorithm name on the chain (not a spec_machine.py field; absent means
DIGEST_ALGORITHM_SHA256); the adapter always removes it. The step goes to
spec_machine.dispatch_tx on a copy of the state, which is kept on a success
or a keep (EXPIRED or REJECTED mark). The response adds "keep" (true for a
keep outcome). Its events are {"type", "id", "policy_version"}: id is the
parameter key of PolicyChanged and the proposal id of ProposalExecuted.

Council rules, each applied only to the step class named and reported in
"deviation". G-1 to G-5 and G-7 are the differences goal 06 recorded
(docs/evidence/goal-06.md, Deviations); G-6 and G-8 to G-11 are further
shape and check-order differences of the same code; authority D-1, D-2 and
D-7 are goal 10's. The scopes of G-6 and G-10 below are the implemented
ones (orchestrator decision ORCH-2, accepted as implemented; not an owner
decision). G-11 is orchestrator decisions ORCH-1 and ORCH-8 (reversible
implementation choices, not owner decisions; open request DR-10-44).
chain/x/authority/README.md lists every rule, and the open differences no
rule names (the oracle maps none of those):

- G-1: a propose whose digest_algorithm is not SHA-256 is MALFORMED once
  the spec's validated_payload got past the INTEGRITY check (it passed, or
  failed later with OUT_OF_RANGE): the chain checks the algorithm right
  after the digest value.
- G-2 (a request mapping): proto3 has no absent integer. A propose without
  base_policy_version or expiry_unix_seconds carries 0, the value the chain
  decodes. The Go generator never omits them; with the rule off the spec
  sees the field absent (MALFORMED).
- G-3: a non-idempotent propose success whose seat already holds
  max_open_proposals_per_seat OPEN or QUEUED proposals on the current base
  version is BOUNDS (the chain's last propose check). The adapter records
  the proposer of each proposal it accepted.
- G-4: a non-idempotent execute success of a validator payload while
  max_pending_changes changes are pending is BOUNDS (checked after the
  projection); the proposal stays QUEUED.
- G-5: a non-idempotent approve success that queues the proposal when
  chain_time + delay_seconds is above unix_seconds_max is OVERFLOW.
- G-6 (exact scope as implemented): when the reset config's council shape
  is not the approved one (the number of seats differs from
  approved_seats, or threshold differs from approved_threshold), every
  council step (propose, approve or execute) during which spec_machine.py
  called council_ready answers DECISION_BLOCKED, whatever code the spec
  then returned (also a later NOT_FOUND or a success), and nothing is
  kept. The rule is checked per step, after the G-2 request mapping and
  before every other council rule; it is not reported when the spec's own
  code was already DECISION_BLOCKED (its council_ready refuses a DEC-008
  threshold above the seat count). A step that stops before council_ready
  (for example a non-AUTHORITY execute, FORBIDDEN or UNAUTHORIZED) is not
  changed. With the approved shape the rule never fires.
- G-7 (a label): a keep outcome commits with ABCI code 0 on the chain and
  carries its code in an authority_result event (DR-06-05). The adapter
  reports the spec's code and keep flag unchanged and names the rule.
- G-8: after a Block, the chain checks the applied validator set again
  (distinct consensus keys, spec power_set_invariant) and halts on a
  failure: the Block answers DUPLICATE_ID or FORBIDDEN and nothing changes.
  A message cannot reach this (every change was projected when scheduled).
- G-9: a validator payload whose activation is unixSeconds outside
  [unix_seconds_min, unix_seconds_max], with a well-formed key, node id and
  reason digest, is BOUNDS on the chain; the spec parses it as MALFORMED.
- G-10 (exact scope as implemented, validator_parse_rule): a propose whose
  spec validated_payload answered MALFORMED, with a payload that is
  exactly {"validatorPower": body}, body keys within consensusPubkey,
  nodeId, reasonDigest, activation and power, both consensusPubkey and
  nodeId present, consensusPubkey the canonical standard base64 of
  consensus_pubkey_bytes bytes, and nodeId outside the id pattern, is
  BOUNDS. reasonDigest, activation and power are not read: absent ones are
  included (the chain checks the node id before them). An absent nodeId
  (the proto3 empty string) is not included; it is G-11 "empty node id".
- G-11 (payload field shape: chain BOUNDS where the spec says MALFORMED):
  a propose whose spec validated_payload answered MALFORMED, with a payload
  that meets one of exactly thirteen conditions (shape_condition), is
  BOUNDS. Both sides reject and neither changes state. Orchestrator
  decision ORCH-1 named the first eight; ORCH-8 (the same open request
  DR-10-44) added the last five, the parse divergences Wave 1 pinned in
  TestUnnamedParseDivergences. The conditions, each with every earlier
  check of the chain's parse passing:
  1. a validator reason digest that names SHA-256 with a value that is
     not 32 bytes (32-byte consensus key, node id in the pattern);
  2. a negative validator power (every earlier validator field well
     formed);
  3. an empty (omitted) consensus key;
  4. a force-withdraw reason digest that names SHA-256 with a value that
     is not 32 bytes (content id in the pattern, version at least 1);
  5. a force-withdraw content id outside the id pattern with a reason
     digest whose algorithm is omitted;
  6. a governed value of 0 (omitted);
  7. an empty (omitted) governed limit key;
  8. an empty provider id in an admit or a disable payload (one parse);
  9. a validator consensus key that is not 32 bytes (not empty) with the
     reason digest omitted;
  10. an empty (omitted) validator node id with a 32-byte consensus key;
  11. a force-withdraw content id outside the id pattern with the reason
      digest omitted;
  12. a force-withdraw content id outside the id pattern with the version
      0 (omitted) and a reason digest the spec parses;
  13. a force-withdraw content id outside the id pattern with a reason
      digest that names SHA-256 with a value that is not 32 bytes.
  shape_condition gives each exactly; G-9, G-10 and G-11 are disjoint.
  The same chain check failing in a combination outside these is not
  mapped (shape_condition lists the open ones).
- authority D-1: whenever the spec's validated_payload passed for a
  governed limit whose key is not in governed_limit_caps or whose value is
  above its ceiling, the chain answers BOUNDS (right after the payload
  checks), whatever later code the spec returns. Applied after G-1.
- authority D-2: a non-idempotent execute success also emits
  ProposalExecuted{proposal_id, policy_version} after PolicyChanged.
- authority D-7: a non-idempotent execute success of a validator payload
  whose consensus key is a council seat key is DUPLICATE_ID (after G-4);
  the proposal stays QUEUED.

The adapter finds where the spec stopped by wrapping spec_machine.py's own
council_ready and validated_payload for the duration of one step (it never
copies their logic). With "council", the projection also carries "council":
height, policy_version, proposals (per id: parameter_key,
base_policy_version, payload_digest, state, expiry_unix_seconds,
execute_after_unix_seconds, approvers as seat ids), executed_params,
governed_limits, validators (node_id, power, consensus_pubkey,
reason_digest), and pending_power in spec activation order.
"""

from __future__ import annotations

import argparse
import base64
import binascii
import copy
import json
import sys
from pathlib import Path
from typing import Any, Final

GOAL02: Final = Path(__file__).resolve().parents[2] / "docs" / "evidence" / "goal-02"
sys.path.insert(0, str(GOAL02))

# spec_machine.py is the frozen, untyped goal-02 specification outside the
# mypy path: every name it exports is Any here.
import spec_machine as sm  # type: ignore[import-not-found]  # noqa: E402

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
MALFORMED: Final = "ERROR_CODE_MALFORMED"
OUT_OF_RANGE: Final = "ERROR_CODE_OUT_OF_RANGE"
OVERFLOW: Final = "ERROR_CODE_OVERFLOW"
DECISION_BLOCKED: Final = "ERROR_CODE_DECISION_BLOCKED"

PROPOSE: Final = "ProposeChange"
APPROVE: Final = "ApproveChange"
EXECUTE: Final = "ExecuteChange"
COUNCIL_OPS: Final = frozenset({PROPOSE, APPROVE, EXECUTE})
COUNCIL_CONFIG_KEYS: Final = frozenset(
    {
        "seats",
        "threshold",
        "delay_seconds",
        "height",
        "validators",
        "approved_seats",
        "approved_threshold",
        "max_open_proposals_per_seat",
        "max_pending_changes",
    }
)
SEAT_CONFIG_KEYS: Final = frozenset({"address", "seat_id", "pubkey"})
VALIDATOR_CONFIG_KEYS: Final = frozenset({"node_id", "power", "consensus_pubkey"})
COUNCIL_RULES: Final = frozenset(
    {
        "G-1",
        "G-2",
        "G-3",
        "G-4",
        "G-5",
        "G-6",
        "G-7",
        "G-8",
        "G-9",
        "G-10",
        "G-11",
        "authority D-1",
        "authority D-2",
        "authority D-7",
    }
)
SHAPE_RULE: Final = "G-11"
SHA256_ALGORITHM: Final = "DIGEST_ALGORITHM_SHA256"
# The integer fields of a propose that proto3 cannot leave absent (G-2).
PROPOSE_INTEGERS: Final = ("base_policy_version", "expiry_unix_seconds")
VALIDATOR_BODY_KEYS: Final = frozenset({"consensusPubkey", "nodeId", "reasonDigest", "activation"})
GOVERNED_BODY_KEYS: Final = frozenset({"limitKey", "value"})
WITHDRAW_BODY_KEYS: Final = frozenset({"contentId", "contentVersion", "reasonDigest"})
DIGEST_KEYS: Final = frozenset({"algorithm", "value"})
# The thirteen G-11 conditions, by the name shape_condition returns: the
# eight of ORCH-1, then the five ORCH-8 added (the Wave 1 rows of
# TestUnnamedParseDivergences).
SHAPE_CONDITIONS: Final = (
    "validator reason digest not 32 bytes",
    "negative validator power",
    "empty consensus key",
    "withdraw reason digest not 32 bytes",
    "withdraw content id outside the pattern, reason without algorithm",
    "governed value 0",
    "empty governed limit key",
    "empty provider id",
    "validator consensus key not 32 bytes, reason digest omitted",
    "empty node id",
    "withdraw content id outside the pattern, reason digest omitted",
    "withdraw content id outside the pattern, version 0",
    "withdraw content id outside the pattern, SHA-256 reason not 32 bytes",
)
OPEN_STATES: Final = frozenset({"OPEN", "QUEUED"})


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


def council_event_ref(event: JSON) -> JSON:
    """A council event: its type, the id it reports, and the policy version."""
    kind = str(event["type"])
    field = {"PolicyChanged": "parameter_key", "ProposalExecuted": "proposal_id"}.get(kind)
    if field is None:
        raise RequestError(f"council event {kind} is not PolicyChanged or ProposalExecuted")
    return {"type": kind, "id": str(event[field]), "policy_version": str(event["policy_version"])}


def activation_view(activation: JSON) -> JSON:
    if activation["kind"] == "height":
        return {"kind": "height", "point": str(activation["height"])}
    return {"kind": "time", "point": str(activation["unix_seconds"])}


def council_projection(state: JSON, seat_ids: frozenset[str]) -> JSON:
    """The council part of the projection (only with the council config)."""
    proposals = []
    for proposal_id in sorted(state["proposals"]):
        p = state["proposals"][proposal_id]
        if any(approver not in seat_ids for approver in p["approvers"]):
            raise RequestError(f"proposal {proposal_id} has an approver that is not a seat")
        proposals.append(
            {
                "proposal_id": proposal_id,
                "parameter_key": p["parameter_key"],
                "base_policy_version": str(p["base_policy_version"]),
                "payload_digest": p["payload_digest"],
                "state": p["state"],
                "expiry_unix_seconds": str(p["expiry_unix_seconds"]),
                "execute_after_unix_seconds": str(p["execute_after_unix_seconds"]),
                "approvers": sorted(p["approvers"]),
            }
        )
    validators = []
    for node_id in sorted(state["validators"]):
        validators.append(
            {
                "node_id": node_id,
                "power": str(state["validators"][node_id]),
                "consensus_pubkey": state["validator_pubkeys"][node_id],
                "reason_digest": state["validator_reasons"].get(node_id),
            }
        )
    pending = []
    for item in sorted(state["pending_power"], key=sm.activation_sort_key):
        pending.append(
            {
                "proposal_id": item["proposal_id"],
                "node_id": item["node_id"],
                "power": str(item["power"]),
                "consensus_pubkey": item["consensus_pubkey"],
                "reason_digest": item["reason_digest"],
                "activation": activation_view(item["activation"]),
            }
        )
    return {
        "height": str(state["height"]),
        "policy_version": str(state["policy_version"]),
        "proposals": proposals,
        "executed_params": [
            {"parameter_key": key, "payload_digest": state["executed_params"][key]}
            for key in sorted(state["executed_params"])
        ],
        "governed_limits": [
            {"limit_key": key, "value": str(state["governed_limits"][key])}
            for key in sorted(state["governed_limits"])
        ],
        "validators": validators,
        "pending_power": pending,
    }


def derived_state(record: JSON, chain_time: int) -> str:
    """Evidence D-4: a RECORDED record reads EXPIRED from its expiry on."""
    stored = str(record["state"])
    if stored == "RECORDED" and record["expiry_unix_seconds"] <= chain_time:
        return "EXPIRED"
    return stored


def projection(state: JSON, evidence: bool, seat_ids: frozenset[str] | None = None) -> JSON:
    """The chain-state projection both sides compare.

    Integers are decimal strings so uint64 values survive JSON exactly.
    seat_ids is set only with the council config; it adds "council".
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
    if seat_ids is not None:
        out["council"] = council_projection(state, seat_ids)
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


def need_hex(value: object, name: str, size: int) -> str:
    text = need_str(value, name)
    if len(text) != 2 * size or any(char not in "0123456789abcdef" for char in text):
        raise RequestError(f"{name} must be {size} bytes of lowercase hex")
    return text


class CouncilConfig:
    """The council genesis and the x/authority chain constants of the council rules."""

    def __init__(self, config: JSON) -> None:
        need_keys(config, COUNCIL_CONFIG_KEYS, "council config")
        seats = config["seats"]
        if not isinstance(seats, list):
            raise RequestError("council seats must be a list")
        # account (hex) -> seat id, and seat id -> council key (hex).
        self.seat_of: dict[str, str] = {}
        self.seat_keys: dict[str, str] = {}
        for index, raw in enumerate(seats):
            seat = need_dict(raw, f"seat {index}")
            need_keys(seat, SEAT_CONFIG_KEYS, f"seat {index}")
            address = need_hex(
                seat["address"], "seat address", int(sm.BOUNDS["account_address_bytes"])
            )
            seat_id = need_str(seat["seat_id"], "seat_id")
            if sm.check_id(seat_id) is not None:
                raise RequestError(f"seat id {seat_id} outside the id pattern")
            if address in self.seat_of or seat_id in self.seat_keys:
                raise RequestError(f"seat {seat_id} ({address}) appears twice")
            self.seat_of[address] = seat_id
            self.seat_keys[seat_id] = need_hex(seat["pubkey"], "seat pubkey", 32)
        if len(set(self.seat_keys.values())) != len(self.seat_keys):
            raise RequestError("two seats share a council key")
        self.threshold = need_int(config["threshold"], "threshold", 1, sm.SAFE_MAX)
        self.delay = need_int(config["delay_seconds"], "delay_seconds", 0, sm.SAFE_MAX)
        self.height = need_int(config["height"], "height", 1, sm.SAFE_MAX)
        validators = config["validators"]
        if not isinstance(validators, list):
            raise RequestError("council validators must be a list")
        self.validators: dict[str, int] = {}
        self.validator_keys: dict[str, str] = {}
        for index, raw in enumerate(validators):
            row = need_dict(raw, f"validator {index}")
            need_keys(row, VALIDATOR_CONFIG_KEYS, f"validator {index}")
            node_id = need_str(row["node_id"], "node_id")
            if node_id in self.validators:
                raise RequestError(f"validator {node_id} appears twice")
            self.validators[node_id] = need_int(row["power"], "power", 1, sm.SAFE_MAX)
            self.validator_keys[node_id] = need_hex(row["consensus_pubkey"], "consensus_pubkey", 32)
        self.approved_seats = need_int(config["approved_seats"], "approved_seats", 1, sm.SAFE_MAX)
        self.approved_threshold = need_int(
            config["approved_threshold"], "approved_threshold", 1, sm.SAFE_MAX
        )
        self.quota = need_int(
            config["max_open_proposals_per_seat"], "max_open_proposals_per_seat", 1, sm.SAFE_MAX
        )
        self.pending_cap = need_int(
            config["max_pending_changes"], "max_pending_changes", 1, sm.SAFE_MAX
        )

    def approved_shape(self) -> bool:
        """G-6: the shape the chain's council check accepts."""
        return (
            len(self.seat_keys) == self.approved_seats and self.threshold == self.approved_threshold
        )


class SpecProbe:
    """Records the first result of chosen spec_machine.py functions during one step.

    The wrapper calls the spec's own function and returns its result
    unchanged, so the spec's outcome never depends on the probe. The module
    attributes are restored when the step ends.
    """

    def __init__(self, names: tuple[str, ...]) -> None:
        self.names = names
        self.seen: dict[str, object] = {}
        self.saved: dict[str, Any] = {}

    def __enter__(self) -> SpecProbe:
        for name in self.names:
            original = getattr(sm, name)
            self.saved[name] = original

            def wrapper(*args: object, _name: str = name, _original: Any = original) -> object:
                result = _original(*args)
                self.seen.setdefault(_name, result)
                return result

            setattr(sm, name, wrapper)
        return self

    def __exit__(self, *exc: object) -> None:
        for name, original in self.saved.items():
            setattr(sm, name, original)

    def reached(self, name: str) -> bool:
        return name in self.seen

    def result(self, name: str) -> object:
        return self.seen.get(name)


def validator_parse_rule(payload: object) -> str | None:
    """G-9 or G-10: the chain's BOUNDS for a validator payload the spec parses as MALFORMED.

    Returns the rule whose condition the payload meets (the caller applies
    it only when the spec's parse answered MALFORMED), or None. The chain
    reads the consensus key, then the node id, the reason digest, the
    activation and the power; spec parse_proposal_payload reads the key
    and then answers MALFORMED for any unparseable power, reason or
    activation before it looks at the node id.
    """
    if not isinstance(payload, dict) or set(payload) != {"validatorPower"}:
        return None
    body = payload["validatorPower"]
    if not isinstance(body, dict) or not set(body) <= VALIDATOR_BODY_KEYS | {"power"}:
        return None
    if not {"consensusPubkey", "nodeId"} <= set(body):
        return None
    raw_key = sm.b64_fixed(body["consensusPubkey"], sm.BOUNDS["consensus_pubkey_bytes"])
    if raw_key is None or sm.standard_b64(raw_key) != body["consensusPubkey"]:
        return None
    if sm.check_id(body["nodeId"]) is not None:
        return "G-10"
    if "reasonDigest" not in body or sm.digest_message(body["reasonDigest"]) is None:
        return None
    activation = body.get("activation")
    if not isinstance(activation, dict) or set(activation) != {"unixSeconds"}:
        return None
    text = activation["unixSeconds"]
    if not isinstance(text, str):
        return None
    try:
        number = int(text, 10)
    except ValueError:
        return None
    if str(number) != text:
        return None
    if int(sm.BOUNDS["unix_seconds_min"]) <= number <= int(sm.BOUNDS["unix_seconds_max"]):
        return None
    return "G-9"


def b64_bytes(value: object) -> bytes | None:
    """The bytes of a proto3 JSON bytes value, or None.

    The Go encoder writes standard base64 with padding and omits an empty
    value, so anything else (not a string, not canonical, or empty) is
    None.
    """
    if not isinstance(value, str):
        return None
    try:
        raw = base64.b64decode(value, validate=True)
    except (binascii.Error, ValueError):
        return None
    if not raw or sm.standard_b64(raw) != value:
        return None
    return raw


def digest_parts(digest: object) -> tuple[str | None, bytes | None] | None:
    """A proto3 JSON Digest as (algorithm name or None, value bytes or None), or None.

    None for the whole result means the object is not a Digest the Go
    encoder can write (not a dict, an unknown key, or a value that is not
    canonical base64). An omitted algorithm (DIGEST_ALGORITHM_UNSPECIFIED)
    or an omitted value (empty bytes) reads as None in its part.
    """
    if not isinstance(digest, dict) or not set(digest) <= DIGEST_KEYS:
        return None
    algorithm = digest.get("algorithm")
    if algorithm is not None and not isinstance(algorithm, str):
        return None
    value = None
    if "value" in digest:
        value = b64_bytes(digest["value"])
        if value is None:
            return None
    return algorithm, value


def sha256_not_32_bytes(digest: object) -> bool:
    """A Digest that names SHA-256 and whose value is not digest_bytes long (empty included)."""
    parts = digest_parts(digest)
    if parts is None or parts[0] != SHA256_ALGORITHM:
        return False
    return parts[1] is None or len(parts[1]) != int(sm.BOUNDS["digest_bytes"])


def without_algorithm(digest: object) -> bool:
    """A Digest whose algorithm is omitted (DIGEST_ALGORITHM_UNSPECIFIED)."""
    parts = digest_parts(digest)
    return parts is not None and parts[0] is None


def negative_int64(value: object) -> bool:
    """A proto3 JSON int64 below zero, in the canonical decimal form the Go encoder writes."""
    if not isinstance(value, str) or not value.startswith("-"):
        return False
    try:
        number = int(value, 10)
    except ValueError:
        return False
    return str(number) == value and -sm.INT64_MAX - 1 <= number < 0


def validator_shape_condition(body: JSON) -> str | None:
    """The G-11 condition of a validatorPower body, or None (see shape_condition)."""
    if not set(body) <= VALIDATOR_BODY_KEYS | {"power"}:
        return None
    if "consensusPubkey" not in body:
        return "empty consensus key"
    key = b64_bytes(body["consensusPubkey"])
    if key is None:
        return None
    if len(key) != int(sm.BOUNDS["consensus_pubkey_bytes"]):
        if "reasonDigest" not in body:
            return "validator consensus key not 32 bytes, reason digest omitted"
        return None
    if "nodeId" not in body:
        return "empty node id"
    node_id = body["nodeId"]
    if not isinstance(node_id, str) or sm.check_id(node_id) is not None:
        return None
    reason = body.get("reasonDigest")
    if sha256_not_32_bytes(reason):
        return "validator reason digest not 32 bytes"
    if sm.digest_message(reason) is None:
        return None
    if "activation" not in body or sm.parse_activation(body["activation"]) is None:
        return None
    if negative_int64(body.get("power")):
        return "negative validator power"
    return None


def withdraw_outside_pattern_condition(body: JSON) -> str | None:
    """The G-11 condition of a forceWithdraw body whose content id is outside the id pattern.

    The chain answers BOUNDS at the content id and reads nothing else; the
    conditions below are the reason digest and version shapes ORCH-1 and
    ORCH-8 name (see shape_condition). The body's content id is non-empty.
    """
    if "reasonDigest" not in body:
        return "withdraw content id outside the pattern, reason digest omitted"
    reason = body["reasonDigest"]
    if without_algorithm(reason):
        return "withdraw content id outside the pattern, reason without algorithm"
    if sha256_not_32_bytes(reason):
        return "withdraw content id outside the pattern, SHA-256 reason not 32 bytes"
    if "contentVersion" not in body and sm.digest_message(reason) is not None:
        return "withdraw content id outside the pattern, version 0"
    return None


def withdraw_shape_condition(body: JSON) -> str | None:
    """The G-11 condition of a forceWithdraw body, or None (see shape_condition)."""
    if not set(body) <= WITHDRAW_BODY_KEYS:
        return None
    content_id = body.get("contentId")
    if not isinstance(content_id, str) or not content_id:
        return None
    if sm.check_id(content_id) is not None:
        return withdraw_outside_pattern_condition(body)
    version = sm.decimal_uint(body.get("contentVersion"))
    if version is None or version < 1:
        return None
    if sha256_not_32_bytes(body.get("reasonDigest")):
        return "withdraw reason digest not 32 bytes"
    return None


def shape_condition(payload: object) -> str | None:
    """G-11: the payload field shape condition the payload meets, or None.

    The payload is proto3 JSON as the Go encoder writes it: a zero integer,
    an empty string or bytes value, and DIGEST_ALGORITHM_UNSPECIFIED are
    omitted. Each condition names the check at which the chain's payload
    parse (chain/x/authority/payload.go) first fails with BOUNDS, with every
    earlier check of that parse passing; spec parse_proposal_payload answers
    MALFORMED for every payload that meets one (the caller applies the rule
    only when the spec answered MALFORMED). The conditions, exactly (the
    first eight are ORCH-1's, the last five ORCH-8's):

    - "validator reason digest not 32 bytes": validatorPower with a 32-byte
      consensus key (canonical base64), a node id in the id pattern, and a
      reason digest that names SHA-256 with a value that is not 32 bytes
      (an omitted value included); activation and power are not read.
    - "negative validator power": validatorPower with a 32-byte consensus
      key, a node id in the id pattern, a reason digest the spec parses, an
      activation the spec parses (the chain accepts exactly those), and a
      negative power.
    - "empty consensus key": validatorPower without consensusPubkey;
      nothing else is read.
    - "withdraw reason digest not 32 bytes": forceWithdraw with a content id
      in the id pattern, a content version of at least 1, and a reason
      digest that names SHA-256 with a value that is not 32 bytes (an
      omitted value included).
    - "withdraw content id outside the pattern, reason without algorithm":
      forceWithdraw with a non-empty content id outside the id pattern and
      a reason digest whose algorithm is omitted; the content version is
      not read.
    - "governed value 0": governedLimit (keys limitKey and value only)
      without value.
    - "empty governed limit key": governedLimit (keys limitKey and value
      only) without limitKey.
    - "empty provider id": admitProvider or disableProvider with an empty
      body (both use the same parse).
    - "validator consensus key not 32 bytes, reason digest omitted":
      validatorPower with a consensus key (canonical base64, not empty)
      that is not 32 bytes and no reasonDigest; nodeId, activation and
      power are not read.
    - "empty node id": validatorPower with a 32-byte consensus key and no
      nodeId; reasonDigest, activation and power are not read.
    - "withdraw content id outside the pattern, reason digest omitted":
      forceWithdraw with a non-empty content id outside the id pattern and
      no reasonDigest; the content version is not read.
    - "withdraw content id outside the pattern, version 0": forceWithdraw
      with a non-empty content id outside the id pattern, no
      contentVersion, and a reason digest the spec parses (SHA-256, 32
      bytes).
    - "withdraw content id outside the pattern, SHA-256 reason not 32
      bytes": forceWithdraw with a non-empty content id outside the id
      pattern and a reason digest that names SHA-256 with a value that is
      not 32 bytes (an omitted value included); the content version is not
      read.

    A body with a key outside its v0 message (a schema version 2 field such
    as activationHeight) meets none. The same check failing with another
    combination meets none either: a consensus key that is not 32 bytes
    with a reason digest present and the node id or the activation
    omitted, and a content id outside the pattern with a reason digest
    whose algorithm is neither omitted nor SHA-256 (a value outside the
    enum, which proto3 JSON writes as a number). Those are open
    divergences that no rule names (TestUnnamedParseDivergences in
    chain/x/authority/difftest pins them and sends one payload of every
    v0 field shape class to show that nothing else is unnamed).
    """
    if not isinstance(payload, dict) or len(payload) != 1:
        return None
    field, body = next(iter(payload.items()))
    if not isinstance(body, dict):
        return None
    if field == "validatorPower":
        return validator_shape_condition(body)
    if field == "forceWithdraw":
        return withdraw_shape_condition(body)
    if field == "governedLimit":
        if not set(body) <= GOVERNED_BODY_KEYS:
            return None
        if "value" not in body:
            return "governed value 0"
        if "limitKey" not in body:
            return "empty governed limit key"
        return None
    if field in ("admitProvider", "disableProvider") and not body:
        return "empty provider id"
    return None


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
        self.governance_cfg: CouncilConfig | None = None
        # proposal id -> the seat id that proposed it (G-3), for every
        # proposal a propose committed.
        self.proposers: dict[str, str] = {}
        # False while a step request asked for no projection.
        self.with_projection = True

    def on(self, rule: str) -> bool:
        return rule not in self.disabled

    def reset(self, config: JSON, disable_deviations: object) -> JSON:
        evidence_config = config.get("evidence")
        council_config = config.get("council")
        need_keys(
            config,
            CONFIG_KEYS
            | ({"evidence"} if evidence_config is not None else frozenset())
            | ({"council"} if council_config is not None else frozenset()),
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
        self.governance_cfg = (
            None if council_config is None else CouncilConfig(need_dict(council_config, "council"))
        )
        if self.governance_cfg is not None:
            self.council_genesis(state, self.governance_cfg)
        self.disabled = self.read_disabled(disable_deviations)
        if self.evidence is not None and self.on("evidence D-3"):
            state["epoch"] = self.evidence.epoch(state["chain_time"])
        self.used_offer_ids = {}
        self.windows = {}
        self.proposers = {}
        self.state = state
        return {"ok": True, "projection": self.project(state)}

    @staticmethod
    def council_genesis(state: JSON, cfg: CouncilConfig) -> None:
        """The council config replaces the spec devnet seats, DEC-008 and validators."""
        for address in cfg.seat_of:
            if address in state["submitters"]:
                raise RequestError(f"seat account {address} is a submitter (DR-06-06)")
        state["seats"] = sorted(cfg.seat_keys)
        dec8 = state["devnet_tests"]["DEC-008"]
        dec8["numeric_value"] = cfg.threshold
        dec8["delay_seconds"] = cfg.delay
        state["height"] = cfg.height
        state["validators"] = dict(cfg.validators)
        state["validator_pubkeys"] = dict(cfg.validator_keys)
        state["validator_reasons"] = {}
        problems = sm.validate_genesis_view(state)
        if problems:
            raise RequestError(f"council genesis: {problems}")

    def project(self, state: JSON) -> JSON:
        seat_ids = None if self.governance_cfg is None else frozenset(self.governance_cfg.seat_keys)
        return projection(state, self.evidence is not None, seat_ids)

    def read_disabled(self, value: object) -> frozenset[str]:
        known = (
            LEASE_RULES
            | (EVIDENCE_RULES if self.evidence is not None else frozenset())
            | (COUNCIL_RULES if self.governance_cfg is not None else frozenset())
        )
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
        if op in COUNCIL_OPS:
            return self.governance(step)
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
            "projection": self.project(self.current()) if self.with_projection else None,
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
        if result["ok"] and self.governance_cfg is not None and self.on("G-8"):
            halt = self.applied_set_error(trial)
            if halt is not None:
                return self.respond(halt, False, [], "G-8")
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

    @staticmethod
    def applied_set_error(state: JSON) -> str | None:
        """G-8: the chain's check of the validator set after a due group."""
        keys = list(state["validator_pubkeys"].values())
        if len(keys) != len(set(keys)):
            return DUPLICATE_ID
        problem = sm.power_set_invariant(state["validators"])
        return None if problem is None else str(problem)

    def council_config(self) -> CouncilConfig:
        if self.governance_cfg is None:
            raise RequestError("a council step needs the council reset config")
        return self.governance_cfg

    def council_request(self, cfg: CouncilConfig, step: JSON) -> tuple[JSON, str, list[str]]:
        """The spec step of a council request, the chain's digest algorithm, and mapping rules.

        Removes the Go-only digest_algorithm field, fills an absent propose
        integer with 0 (G-2), and replaces a seat account signer of a
        propose or an approve with its seat id.
        """
        op = step["op"]
        spec = dict(step)
        algorithm = SHA256_ALGORITHM
        rules: list[str] = []
        if "digest_algorithm" in spec:
            if op != PROPOSE:
                raise RequestError("digest_algorithm belongs to a propose")
            algorithm = need_str(spec.pop("digest_algorithm"), "digest_algorithm")
        if op == PROPOSE and self.on("G-2"):
            for name in PROPOSE_INTEGERS:
                if name not in spec:
                    spec[name] = 0
                    if "G-2" not in rules:
                        rules.append("G-2")
        signer = spec.get("signer")
        if op in (PROPOSE, APPROVE) and isinstance(signer, str) and signer in cfg.seat_of:
            spec["signer"] = cfg.seat_of[signer]
        return spec, algorithm, rules

    def open_count(self, state: JSON, seat_id: str) -> int:
        """G-3: OPEN or QUEUED proposals of seat_id on the current base version."""
        return sum(
            1
            for proposal_id, proposal in state["proposals"].items()
            if proposal["state"] in OPEN_STATES
            and proposal["base_policy_version"] == state["policy_version"]
            and self.proposers.get(proposal_id) == seat_id
        )

    def governance(self, step: JSON) -> JSON:
        """One council transaction: spec_machine.py plus the council rules, in the chain's order."""
        cfg = self.council_config()
        pre = self.current()
        before = len(pre["events"])
        op = step["op"]
        spec, algorithm, rules = self.council_request(cfg, step)
        trial = copy.deepcopy(pre)
        with SpecProbe(("council_ready", "validated_payload")) as probe:
            result = sm.dispatch_tx(trial, spec)
        ok = bool(result["ok"])
        idempotent = bool(result["idempotent"])
        keep = bool(result["keep"]) and not ok
        code = "ok" if ok else str(result["error"])

        def refuse(rule: str, chain_code: str) -> JSON:
            return self.council_respond(chain_code, False, False, [], rules + [rule])

        if self.on("G-6") and probe.reached("council_ready") and not cfg.approved_shape():
            if code != DECISION_BLOCKED:
                return refuse("G-6", DECISION_BLOCKED)
        if op == PROPOSE:
            mapped = self.propose_rules(cfg, pre, spec, algorithm, probe, ok and not idempotent)
            if mapped is not None:
                return refuse(*mapped)
        elif op == APPROVE and ok and not idempotent and self.on("G-5"):
            proposal = trial["proposals"][spec["proposal_id"]]
            queued = proposal["state"] == "QUEUED"
            if queued and pre["chain_time"] + cfg.delay > int(sm.BOUNDS["unix_seconds_max"]):
                return refuse("G-5", OVERFLOW)
        elif op == EXECUTE and ok and not idempotent:
            payload = pre["proposals"][spec["proposal_id"]]["payload"]
            if payload["kind"] == "validator_power":
                if self.on("G-4") and len(pre["pending_power"]) >= cfg.pending_cap:
                    return refuse("G-4", BOUNDS_CODE)
                seat_keys = set(cfg.seat_keys.values())
                if self.on("authority D-7") and payload["consensus_pubkey"] in seat_keys:
                    return refuse("authority D-7", DUPLICATE_ID)
        if not result["keep"]:
            return self.council_respond(code, False, False, [], rules)
        self.state = trial
        events = list(trial["events"][before:])
        if op == PROPOSE and ok and not idempotent:
            self.proposers[spec["proposal_id"]] = spec["signer"]
        if op == EXECUTE and ok and not idempotent and self.on("authority D-2"):
            events.append(
                {
                    "type": "ProposalExecuted",
                    "proposal_id": spec["proposal_id"],
                    "policy_version": trial["policy_version"],
                }
            )
            rules.append("authority D-2")
        if keep and self.on("G-7"):
            rules.append("G-7")
        return self.council_respond(code, idempotent, keep, events, rules)

    def propose_rules(
        self,
        cfg: CouncilConfig,
        pre: JSON,
        spec: JSON,
        algorithm: str,
        probe: SpecProbe,
        fresh_success: bool,
    ) -> tuple[str, str] | None:
        """The chain's propose outcome where it differs from the spec: (rule, code), or None.

        In the chain's order: G-9, G-10 and G-11 inside the payload parse
        (their conditions are disjoint), G-1 after the digest value,
        authority D-1 after validated_payload, and G-3 last.
        """
        if probe.reached("validated_payload"):
            checked = probe.result("validated_payload")
            if not isinstance(checked, dict):
                raise RequestError("validated_payload returned a non-object")
            failed = "ok" in checked
            spec_code = str(checked["error"]) if failed else ""
            if failed and spec_code == MALFORMED:
                payload = spec.get("payload")
                rule = validator_parse_rule(payload)
                if rule is None and shape_condition(payload) is not None:
                    rule = SHAPE_RULE
                if rule is not None and self.on(rule):
                    return rule, BOUNDS_CODE
            if algorithm != SHA256_ALGORITHM and self.on("G-1"):
                if not failed or spec_code == OUT_OF_RANGE:
                    return "G-1", MALFORMED
            if not failed and checked["kind"] == "governed_limit" and self.on("authority D-1"):
                ceiling = sm.BOUNDS["governed_limit_caps"].get(checked["limit_key"])
                if ceiling is None or checked["value"] > int(ceiling):
                    return "authority D-1", BOUNDS_CODE
        if fresh_success and self.on("G-3"):
            if self.open_count(pre, spec["signer"]) >= cfg.quota:
                return "G-3", BOUNDS_CODE
        return None

    def council_respond(
        self, code: str, idempotent: bool, keep: bool, events: list[JSON], rules: list[str]
    ) -> JSON:
        return {
            "code": code,
            "idempotent": idempotent,
            "keep": keep,
            "events": [council_event_ref(event) for event in events],
            "deviation": "+".join(rules) or None,
            "projection": self.project(self.current()) if self.with_projection else None,
        }

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
            keys = frozenset({"cmd", "step"})
            if "projection" in req:
                keys |= {"projection"}
                if not isinstance(req["projection"], bool):
                    raise RequestError("projection must be a boolean")
            need_keys(req, keys, "step")
            oracle.with_projection = req.get("projection", True) is True
            try:
                return oracle.step(need_dict(req["step"], "step"))
            finally:
                oracle.with_projection = True
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
    step: JSON = sm.offer(offer_id, content_id=content_id, expiry=expiry)
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
    step: JSON = sm.accept(offer_id, lease_id)
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


# Council self-test. Four seats with accounts 0xa1..0xa4 (not submitters),
# six validators of power 10, DEC-008 threshold 3. A council case is (step,
# code with every rule on, deviation, code with the segment's rules off,
# deviation then).
CouncilCase = tuple[JSON, str, str | None, str, str | None]
SEATS: Final = [
    (sm.addr(0xA1 + i), f"seat-{i + 1}", sm.pubkey_for(f"seat-{i + 1}")) for i in range(4)
]
VALIDATORS: Final = [(f"v{i}", 10, sm.pubkey_for(f"v{i}")) for i in range(1, 7)]
D2: Final = "authority D-2"
UNIX_MAX: Final = int(sm.BOUNDS["unix_seconds_max"])


def council_config(
    threshold: int = 3, delay: int = 10, quota: int = 32, pending_cap: int = 128
) -> JSON:
    config = self_config(1)
    config["council"] = {
        "seats": [{"address": a, "seat_id": s, "pubkey": k} for a, s, k in SEATS],
        "threshold": threshold,
        "delay_seconds": delay,
        "height": 1,
        "validators": [{"node_id": n, "power": p, "consensus_pubkey": k} for n, p, k in VALIDATORS],
        "approved_seats": 4,
        "approved_threshold": 3,
        "max_open_proposals_per_seat": quota,
        "max_pending_changes": pending_cap,
    }
    return config


def default_parameter_key(payload: JSON) -> str:
    field, body = next(iter(payload.items()))
    if field == "governedLimit":
        return str(body["limitKey"])
    if field == "validatorPower":
        return "validator-" + str(body["nodeId"])
    if field == "admitProvider":
        return "admit-" + str(body["providerId"])
    if field == "disableProvider":
        return "disable-" + str(body["providerId"])
    return "withdraw-" + str(body["contentId"])


def propose_step(
    seat: int,
    proposal_id: str,
    payload: JSON,
    base: int = 1,
    expiry: int = T + 10000,
    business: str | None = None,
    algorithm: str | None = None,
    digest: str | None = None,
) -> JSON:
    step: JSON = {
        "op": PROPOSE,
        "signer": SEATS[seat - 1][0],
        "business_id": business or f"b-{proposal_id}",
        "proposal_id": proposal_id,
        "parameter_key": default_parameter_key(payload),
        "base_policy_version": base,
        "payload": payload,
        "payload_digest": digest or sm.payload_digest(payload),
        "expiry_unix_seconds": expiry,
    }
    if algorithm is not None:
        step["digest_algorithm"] = algorithm
    return step


def approve_step(seat: int, proposal_id: str, business: str | None = None) -> JSON:
    return {
        "op": APPROVE,
        "signer": SEATS[seat - 1][0],
        "business_id": business or f"a-{proposal_id}-{seat}",
        "proposal_id": proposal_id,
    }


def execute_step(proposal_id: str, signer: str = sm.AUTHORITY, business: str | None = None) -> JSON:
    return {
        "op": EXECUTE,
        "signer": signer,
        "business_id": business or f"x-{proposal_id}",
        "proposal_id": proposal_id,
    }


def same(step: JSON, code: str, deviation: str | None = None) -> CouncilCase:
    return (step, code, deviation, code, deviation)


def governed(key: str, value: int) -> JSON:
    payload: JSON = sm.governed_limit_payload(key, value)
    return payload


def add_validator(node_id: str, height: int, pubkey: str | None = None, power: int = 10) -> JSON:
    payload: JSON = sm.validator_power_payload(node_id, power, {"height": str(height)}, pubkey)
    return payload


def raw_validator(node_id: str, activation: JSON, reason: JSON | None = None) -> JSON:
    payload: JSON = sm.validator_power_payload(node_id, 10, activation)
    if reason is not None:
        payload["validatorPower"]["reasonDigest"] = reason
    return payload


def no_reason(node_id: str) -> JSON:
    """A validator payload without its reason digest (proto3 JSON omits an unset message)."""
    payload = raw_validator(node_id, {"height": "9"})
    del payload["validatorPower"]["reasonDigest"]
    return payload


def passed(
    proposal_id: str, payload: JSON, base: int, at: int | None, seat: int = 1
) -> list[CouncilCase]:
    """Propose, three approvals, a block at time at (None: no block), and the execute."""
    cases = [same(propose_step(seat, proposal_id, payload, base), "ok")]
    cases += [same(approve_step(s, proposal_id), "ok") for s in (1, 2, 3)]
    if at is not None:
        cases.append(same({"op": "Block", "header_time": at}, "ok"))
    cases.append(same(execute_step(proposal_id), "ok", D2))
    return cases


def flow_segment() -> list[CouncilCase]:
    """Authority D-2 and G-7 on the plain council flow (delay 10)."""
    p1 = governed("max_page_limit", 50)
    return [
        same(propose_step(1, "p1", p1), "ok"),
        same(approve_step(1, "p1"), "ok"),
        same(approve_step(2, "p1"), "ok"),
        same(approve_step(2, "p1", "a-p1-2b"), "ERROR_CODE_REPLAY"),
        same(approve_step(3, "p1"), "ok"),
        same(approve_step(4, "p1"), CONFLICT),
        same(execute_step("p1"), OUT_OF_RANGE),
        same(execute_step("p1", sm.SERVICE, "x-p1-svc"), FORBIDDEN),
        same(execute_step("p1", SEATS[0][0], "x-p1-seat"), UNAUTHORIZED),
        same({"op": "Block", "header_time": T + 10}, "ok"),
        (execute_step("p1"), "ok", D2, "ok", None),
        same(execute_step("p1"), "ok"),
        same(propose_step(2, "p2", governed("max_page_limit", 60)), "ERROR_CODE_STALE_VERSION"),
        same(propose_step(2, "p3", governed("max_open_offers", 7), 2, T + 20), "ok"),
        same({"op": "Block", "header_time": T + 20}, "ok"),
        (approve_step(1, "p3"), EXPIRED, "G-7", EXPIRED, None),
        (execute_step("p3"), EXPIRED, "G-7", EXPIRED, None),
        same({"op": "Block", "header_time": T + 10000}, "ok"),
        # An EXECUTED proposal executed again after its expiry is relabelled.
        (execute_step("p1", business="x-p1-late"), EXPIRED, "G-7", EXPIRED, None),
    ]


def d1_segment() -> list[CouncilCase]:
    over = governed("max_page_limit", 101)
    return [
        (
            propose_step(1, "d1", governed("max_tx_bytes", 5)),
            BOUNDS_CODE,
            "authority D-1",
            "ok",
            None,
        ),
        (propose_step(1, "d2", over), BOUNDS_CODE, "authority D-1", "ok", None),
        (propose_step(1, "d3", over, expiry=T), BOUNDS_CODE, "authority D-1", EXPIRED, None),
        (
            propose_step(1, "d4", governed("max_open_offers", 1025), business="b-d2"),
            BOUNDS_CODE,
            "authority D-1",
            "ERROR_CODE_REPLAY",
            None,
        ),
        same(propose_step(1, "d5", governed("max_page_limit", 100)), "ok"),
    ]


def g1_segment() -> list[CouncilCase]:
    unspecified = "DIGEST_ALGORITHM_UNSPECIFIED"
    past = add_validator("v7", 1)
    return [
        (
            propose_step(1, "g1", governed("max_page_limit", 50), algorithm=unspecified),
            MALFORMED,
            "G-1",
            "ok",
            None,
        ),
        same(
            propose_step(
                1, "g2", governed("max_page_limit", 50), algorithm=unspecified, digest="00" * 32
            ),
            "ERROR_CODE_INTEGRITY",
        ),
        (propose_step(1, "g3", past, algorithm=unspecified), MALFORMED, "G-1", OUT_OF_RANGE, None),
        (
            propose_step(1, "g4", governed("max_tx_bytes", 5), algorithm=unspecified),
            MALFORMED,
            "G-1",
            BOUNDS_CODE,
            "authority D-1",
        ),
    ]


def g2_segment() -> list[CouncilCase]:
    no_expiry = propose_step(1, "n1", governed("max_page_limit", 50))
    del no_expiry["expiry_unix_seconds"]
    no_base = propose_step(1, "n2", governed("max_page_limit", 50))
    del no_base["base_policy_version"]
    return [
        (no_expiry, BOUNDS_CODE, "G-2", MALFORMED, None),
        (no_base, "ERROR_CODE_STALE_VERSION", "G-2", MALFORMED, None),
    ]


def parse_segment() -> list[CouncilCase]:
    """G-9 and G-10 (both off in the second run)."""
    no_algorithm = {"value": sm.digest_json(sm.REASON)["value"]}
    return [
        (
            propose_step(1, "u1", raw_validator("v7", {"unixSeconds": "0"})),
            BOUNDS_CODE,
            "G-9",
            MALFORMED,
            None,
        ),
        (
            propose_step(1, "u2", raw_validator("v7", {"unixSeconds": str(UNIX_MAX + 1)})),
            BOUNDS_CODE,
            "G-9",
            MALFORMED,
            None,
        ),
        (
            propose_step(1, "u3", raw_validator("v7", {"unixSeconds": "-5"})),
            BOUNDS_CODE,
            "G-9",
            MALFORMED,
            None,
        ),
        same(propose_step(1, "u4", raw_validator("v7", {"height": "0"})), MALFORMED),
        # "-bad" is outside the id pattern; its parameter key "validator--bad" is not.
        same(propose_step(1, "u5", raw_validator("-bad", {"height": "9"})), BOUNDS_CODE),
        (
            propose_step(1, "u6", raw_validator("-bad", {"unixSeconds": "0"})),
            BOUNDS_CODE,
            "G-10",
            MALFORMED,
            None,
        ),
        (
            propose_step(1, "u7", raw_validator("-bad", {"height": "9"}, no_algorithm)),
            BOUNDS_CODE,
            "G-10",
            MALFORMED,
            None,
        ),
        same(propose_step(1, "u8", raw_validator("v7", {"height": "9"}, no_algorithm)), MALFORMED),
        (propose_step(1, "u9", no_reason("-bad")), BOUNDS_CODE, "G-10", MALFORMED, None),
        same(propose_step(1, "u10", no_reason("v7")), MALFORMED),
    ]


def shape_step(proposal_id: str, payload: JSON, parameter_key: str) -> JSON:
    """A propose with an explicit parameter key (the payload may omit the field it names)."""
    step = propose_step(
        1, proposal_id, {"governedLimit": {"limitKey": "max_page_limit", "value": "1"}}
    )
    step["payload"] = payload
    step["parameter_key"] = parameter_key
    step["payload_digest"] = sm.payload_digest(payload)
    return step


def short_key_validator() -> JSON:
    """A validator payload whose consensus key is 31 bytes, every other field well formed."""
    payload: JSON = sm.validator_power_payload("v7", 10, {"height": "9"})
    payload["validatorPower"]["consensusPubkey"] = sm.standard_b64(
        bytes.fromhex(sm.pubkey_for("v7"))[:31]
    )
    return payload


def without(payload: JSON, *fields: str) -> JSON:
    """A copy of a one-field payload without the named body fields (proto3 JSON omits defaults)."""
    out = copy.deepcopy(payload)
    body = next(iter(out.values()))
    for name in fields:
        del body[name]
    return out


def shape_payloads() -> list[tuple[str, JSON, str]]:
    """Payloads of every G-11 condition (two for some): (condition, payload, key)."""
    reason = sm.digest_json(sm.REASON)
    short = sm.standard_b64(bytes.fromhex(sm.REASON)[:31])
    sha_short = {"algorithm": SHA256_ALGORITHM, "value": short}
    sha_empty = {"algorithm": SHA256_ALGORITHM}
    no_algorithm = {"value": reason["value"]}
    negative = sm.validator_power_payload("v7", 10, {"height": "9"})
    negative["validatorPower"]["power"] = "-5"
    no_key = without(sm.validator_power_payload("v7", 10, {"height": "9"}), "consensusPubkey")
    full = sm.validator_power_payload("v7", 10, {"height": "9"})
    return [
        (SHAPE_CONDITIONS[0], raw_validator("v7", {"height": "9"}, sha_short), "validator-v7"),
        (SHAPE_CONDITIONS[0], raw_validator("v7", {"height": "9"}, sha_empty), "validator-v7"),
        (SHAPE_CONDITIONS[1], negative, "validator-v7"),
        (SHAPE_CONDITIONS[2], no_key, "validator-v7"),
        (SHAPE_CONDITIONS[3], withdraw_body("c1", "1", sha_short), "withdraw-c1"),
        (SHAPE_CONDITIONS[4], withdraw_body("-bad", "1", no_algorithm), "withdraw-x"),
        (SHAPE_CONDITIONS[5], {"governedLimit": {"limitKey": "max_page_limit"}}, "max_page_limit"),
        (SHAPE_CONDITIONS[6], {"governedLimit": {"value": "5"}}, "max_page_limit"),
        (SHAPE_CONDITIONS[7], {"admitProvider": {}}, "admit-x"),
        (SHAPE_CONDITIONS[7], {"disableProvider": {}}, "disable-x"),
        (SHAPE_CONDITIONS[8], without(short_key_validator(), "reasonDigest"), "validator-v7"),
        (
            SHAPE_CONDITIONS[8],
            without(short_key_validator(), "reasonDigest", "nodeId"),
            "validator-v7",
        ),
        (SHAPE_CONDITIONS[9], without(full, "nodeId"), "validator-v7"),
        (SHAPE_CONDITIONS[9], without(negative, "nodeId", "reasonDigest"), "validator-v7"),
        (SHAPE_CONDITIONS[10], withdraw_body("-bad", "1", None), "withdraw-x"),
        (SHAPE_CONDITIONS[10], withdraw_body("-bad", None, None), "withdraw-x"),
        (SHAPE_CONDITIONS[11], withdraw_body("-bad", None, reason), "withdraw-x"),
        (SHAPE_CONDITIONS[12], withdraw_body("-bad", "1", sha_short), "withdraw-x"),
        (SHAPE_CONDITIONS[12], withdraw_body("-bad", None, sha_empty), "withdraw-x"),
    ]


def withdraw_body(content_id: str | None, version: str | None, reason: JSON | None) -> JSON:
    """A forceWithdraw payload; None omits the field (as proto3 JSON omits a default)."""
    body: JSON = {}
    for name, value in (
        ("contentId", content_id),
        ("contentVersion", version),
        ("reasonDigest", reason),
    ):
        if value is not None:
            body[name] = value
    return {"forceWithdraw": body}


def shape_controls() -> list[tuple[JSON, str, str]]:
    """Near misses of the G-11 conditions that the rule must leave alone: (payload, key, code).

    Each answers the same code with the rule on and off. In order: nine
    payloads the chain also answers MALFORMED for (a reason digest without
    algorithm, 32 or 31 bytes, in a validator and in a withdraw payload; a
    withdraw version 0 and a withdraw reason digest omitted, both with a
    content id in the pattern; a withdraw with the content id omitted; a
    negative power with an activation height of 0, which the chain refuses
    before the power; a 32-byte consensus key with the reason digest
    omitted), checked against the chain by TestShapeRuleNearMisses; three
    the spec also answers BOUNDS for (the last a 31-byte consensus key with
    every field present); four payloads of the three open divergence
    classes TestUnnamedParseDivergences pins (the chain answers BOUNDS, no
    rule maps them; the last two differ only in the version, which no
    condition of a content id outside the pattern reads unless the reason
    digest parses); and two schema version 2 governed bodies (D-4, D-5;
    TestSchemaTwoPayloadsD5).
    """
    reason = sm.digest_json(sm.REASON)
    short = sm.standard_b64(bytes.fromhex(sm.REASON)[:31])
    no_algorithm = {"value": reason["value"]}
    short_no_algorithm = {"value": short}
    # A Digest algorithm outside the enum: proto3 JSON writes its number.
    unknown_algorithm = {"algorithm": 7, "value": reason["value"]}
    negative_height_zero = sm.validator_power_payload("v7", 10, {"height": "0"})
    negative_height_zero["validatorPower"]["power"] = "-5"
    no_reason_full = without(sm.validator_power_payload("v7", 10, {"height": "9"}), "reasonDigest")
    scheduled = {"activationHeight": "500", "limitKey": "max_page_limit", "value": "5"}
    scheduled_no_value = {"activationHeight": "500", "limitKey": "max_page_limit"}
    return [
        (raw_validator("v7", {"height": "9"}, no_algorithm), "validator-v7", MALFORMED),
        (raw_validator("v7", {"height": "9"}, short_no_algorithm), "validator-v7", MALFORMED),
        (withdraw_body("c1", "1", no_algorithm), "withdraw-c1", MALFORMED),
        (withdraw_body("c1", "1", short_no_algorithm), "withdraw-c1", MALFORMED),
        (withdraw_body("c1", None, reason), "withdraw-c1", MALFORMED),
        (withdraw_body("c1", "1", None), "withdraw-c1", MALFORMED),
        (withdraw_body(None, "1", reason), "withdraw-c1", MALFORMED),
        (negative_height_zero, "validator-v7", MALFORMED),
        (no_reason_full, "validator-v7", MALFORMED),
        ({"governedLimit": {"limitKey": "-bad", "value": "5"}}, "max_page_limit", BOUNDS_CODE),
        (withdraw_body("-bad", "1", reason), "withdraw-x", BOUNDS_CODE),
        (short_key_validator(), "validator-v7", BOUNDS_CODE),
        (without(short_key_validator(), "nodeId"), "validator-v7", MALFORMED),
        (without(short_key_validator(), "activation"), "validator-v7", MALFORMED),
        (withdraw_body("-bad", "1", unknown_algorithm), "withdraw-x", MALFORMED),
        (withdraw_body("-bad", None, unknown_algorithm), "withdraw-x", MALFORMED),
        ({"governedLimit": scheduled}, "max_page_limit", MALFORMED),
        ({"governedLimit": scheduled_no_value}, "max_page_limit", MALFORMED),
    ]


def shape_segment() -> list[CouncilCase]:
    """G-11 on every condition, then the near misses it must leave alone."""
    cases: list[CouncilCase] = []
    for index, (_, payload, key) in enumerate(shape_payloads()):
        cases.append(
            (shape_step(f"f{index}", payload, key), BOUNDS_CODE, SHAPE_RULE, MALFORMED, None)
        )
    for index, (payload, key, code) in enumerate(shape_controls()):
        cases.append(same(shape_step(f"n{index}", payload, key), code))
    return cases


def shape_condition_self_test() -> int:
    """Each G-11 row meets exactly its condition, and every near miss meets none."""
    rows = shape_payloads()
    for condition, payload, _ in rows:
        if shape_condition(payload) != condition:
            raise AssertionError(f"{payload} meets {shape_condition(payload)!r}, not {condition!r}")
        if validator_parse_rule(payload) is not None:
            raise AssertionError(f"a G-11 payload {payload} also meets G-9 or G-10")
    if sorted({condition for condition, _, _ in rows}) != sorted(SHAPE_CONDITIONS):
        raise AssertionError("a G-11 condition has no self-test payload")
    for payload, _, _ in shape_controls():
        if shape_condition(payload) is not None:
            raise AssertionError(f"near miss {payload} meets {shape_condition(payload)!r}")
    for payload in (
        raw_validator("-bad", {"unixSeconds": "0"}),
        raw_validator("v7", {"unixSeconds": "0"}),
    ):
        if shape_condition(payload) is not None:
            raise AssertionError(f"a G-9 or G-10 payload {payload} meets a G-11 condition")
    return len(rows) + len(shape_controls()) + 2


def g3_segment() -> list[CouncilCase]:
    """G-3 with a quota of 2 (delay 10)."""
    return [
        same(propose_step(4, "q1", governed("max_page_limit", 11)), "ok"),
        same(propose_step(4, "q2", governed("max_page_limit", 12)), "ok"),
        (propose_step(4, "q3", governed("max_page_limit", 13)), BOUNDS_CODE, "G-3", "ok", None),
        same(propose_step(3, "q4", governed("max_page_limit", 14)), "ok"),
        *passed("q5", governed("max_open_offers", 9), 1, T + 10),
        same(propose_step(4, "q6", governed("max_page_limit", 15), 2), "ok"),
    ]


def g4_segment() -> list[CouncilCase]:
    """G-4 with a pending cap of 2 (delay 0, so no block is needed)."""
    last = passed("v9", add_validator("v9", 102), 3, None)
    head, execute = last[:-1], last[-1][0]
    return [
        *passed("v7", add_validator("v7", 100), 1, None),
        *passed("v8", add_validator("v8", 101), 2, None),
        *head,
        (execute, BOUNDS_CODE, "G-4", "ok", D2),
    ]


def d7_segment() -> list[CouncilCase]:
    """Authority D-7: a validator payload with a seat key (delay 0)."""
    last = passed("v7", add_validator("v7", 100, SEATS[0][2]), 1, None)
    head, execute = last[:-1], last[-1][0]
    return [
        *head,
        (execute, DUPLICATE_ID, "authority D-7", "ok", D2),
        # The proposal stays QUEUED: the same refusal again.
        (execute_step("v7", business="x-v7-2"), DUPLICATE_ID, "authority D-7", CONFLICT, None),
    ]


def g5_segment() -> list[CouncilCase]:
    near = UNIX_MAX - 5
    return [
        same({"op": "Block", "header_time": near}, "ok"),
        same(propose_step(1, "o1", governed("max_page_limit", 50), expiry=UNIX_MAX), "ok"),
        same(approve_step(1, "o1"), "ok"),
        same(approve_step(2, "o1"), "ok"),
        (approve_step(3, "o1"), OVERFLOW, "G-5", "ok", None),
    ]


def g6_segment() -> list[CouncilCase]:
    """G-6 with DEC-008 threshold 2 on four seats."""
    return [
        (
            propose_step(1, "s1", governed("max_page_limit", 50)),
            DECISION_BLOCKED,
            "G-6",
            "ok",
            None,
        ),
        same(execute_step("s1", sm.SERVICE), FORBIDDEN),
        (execute_step("none"), DECISION_BLOCKED, "G-6", NOT_FOUND, None),
    ]


def run_council(oracle: Oracle, cases: list[CouncilCase], rules_on: bool) -> list[JSON]:
    trace: list[Case] = [
        (step, on if rules_on else off, dev_on if rules_on else dev_off)
        for step, on, dev_on, off, dev_off in cases
    ]
    return run_trace(oracle, trace)


def council_segments() -> list[tuple[str, JSON, list[str], list[CouncilCase]]]:
    return [
        ("flow", council_config(), [D2, "G-7"], flow_segment()),
        ("D-1", council_config(), ["authority D-1"], d1_segment()),
        ("G-1", council_config(), ["G-1"], g1_segment()),
        ("G-2", council_config(), ["G-2"], g2_segment()),
        ("G-9/G-10", council_config(), ["G-9", "G-10"], parse_segment()),
        ("G-11", council_config(), [SHAPE_RULE], shape_segment()),
        ("G-3", council_config(quota=2), ["G-3"], g3_segment()),
        ("G-4", council_config(delay=0, pending_cap=2), ["G-4"], g4_segment()),
        ("D-7", council_config(delay=0), ["authority D-7"], d7_segment()),
        ("G-5", council_config(), ["G-5"], g5_segment()),
        ("G-6", council_config(threshold=2), ["G-6"], g6_segment()),
    ]


def g8_self_test() -> int:
    """G-8: a due set that breaks the power invariant or repeats a key halts the Block.

    No message can build such a pending set (every change is projected when
    it is scheduled), so the self-test writes the pending entry into the
    spec state directly; the step under test is the Block.
    """
    reason = sm.REASON
    broken = [
        ("v9", 1000, sm.pubkey_for("v9"), "ERROR_CODE_FORBIDDEN"),
        ("v9", 10, VALIDATORS[0][2], DUPLICATE_ID),
    ]
    for node_id, power, pubkey, code in broken:
        for rules_on in (True, False):
            oracle = Oracle()
            reset(oracle, council_config(), disable=False if rules_on else ["G-8"])
            oracle.current()["pending_power"].append(
                {
                    "proposal_id": "injected",
                    "node_id": node_id,
                    "consensus_pubkey": pubkey,
                    "power": power,
                    "reason_digest": reason,
                    "activation": {"kind": "height", "height": 2},
                }
            )
            block = {"op": "Block", "header_time": T + 1}
            expected = (code, "G-8") if rules_on else ("ok", None)
            responses = run_trace(oracle, [(block, *expected)])
            pending = responses[0]["projection"]["council"]["pending_power"]
            if rules_on and [p["proposal_id"] for p in pending] != ["injected"]:
                raise AssertionError("a G-8 halt changed the state")
    return 4


def council_self_test() -> int:
    steps = 0
    for name, config, rules, cases in council_segments():
        oracle = Oracle()
        reset(oracle, config, disable=False)
        try:
            responses = run_council(oracle, cases, rules_on=True)
        except AssertionError as exc:
            raise AssertionError(f"council segment {name}, rules on: {exc}") from exc
        if name == "flow":
            check_flow(responses, d2_on=True)
        oracle = Oracle()
        reset(oracle, config, disable=rules)
        try:
            off = run_council(oracle, cases, rules_on=False)
        except AssertionError as exc:
            raise AssertionError(f"council segment {name}, {rules} off: {exc}") from exc
        if name == "flow":
            check_flow(off, d2_on=False)
        steps += 2 * len(cases)
    steps += g8_self_test()
    steps += shape_condition_self_test()
    # A council step needs the council config, and council rule names are
    # unknown without it.
    oracle = Oracle()
    reset(oracle, self_config(1), disable=False)
    refused = json.loads(
        handle_line(oracle, json.dumps({"cmd": "step", "step": execute_step("p1")}))
    )
    if "error" not in refused:
        raise AssertionError("a council step without the council config was not an error")
    bad = json.loads(
        handle_line(
            Oracle(),
            json.dumps({"cmd": "reset", "config": self_config(1), "disable_deviations": ["G-1"]}),
        )
    )
    if "error" not in bad:
        raise AssertionError("a council rule name was accepted without the council config")
    plain = json.loads(
        handle_line(
            Oracle(),
            json.dumps({"cmd": "reset", "config": self_config(1), "disable_deviations": False}),
        )
    )
    if "council" in plain["projection"]:
        raise AssertionError("the lease projection carries a council part")
    return steps


def check_flow(responses: list[JSON], d2_on: bool) -> None:
    """The typed events and keep flags of the flow segment."""
    executed = responses[10]
    want = [{"type": "PolicyChanged", "id": "max_page_limit", "policy_version": "2"}]
    if d2_on:
        want.append({"type": "ProposalExecuted", "id": "p1", "policy_version": "2"})
    council = executed["projection"]["council"]
    checks = {
        "execute events": executed["events"] == want,
        "idempotent execute": responses[11]["idempotent"] is True and responses[11]["events"] == [],
        "keep flags": [responses[i]["keep"] for i in (15, 16, 18)] == [True, True, True],
        "no keep on a refusal": responses[6]["keep"] is False,
        "governed value": council["governed_limits"]
        == [
            {"limit_key": "max_expiry_steps", "value": "1"},
            {"limit_key": "max_page_limit", "value": "50"},
        ],
        "approvers": council["proposals"][0]["approvers"] == ["seat-1", "seat-2", "seat-3"],
        "relabel": [p["state"] for p in responses[-1]["projection"]["council"]["proposals"]]
        == ["EXPIRED", "EXPIRED"],
    }
    failed = [name for name, held in checks.items() if not held]
    if failed:
        raise AssertionError(f"council flow checks failed: {failed}")


def self_test() -> int:
    steps = lease_self_test() + evidence_self_test() + council_self_test()
    # A bad request is an error response, not a code.
    oracle = Oracle()
    reset(oracle, self_config(1), disable=False)
    bad = json.loads(handle_line(oracle, json.dumps({"cmd": "step", "step": {"op": "Nope"}})))
    if "error" not in bad:
        raise AssertionError("unknown op was not an error")
    # "projection": false omits only the projection.
    step = {"cmd": "step", "step": sm.register_provider()}
    brief = json.loads(handle_line(oracle, json.dumps({**step, "projection": False})))
    if brief.get("projection", "absent") is not None or brief.get("code") != "ok":
        raise AssertionError(f"a step without projection answered {brief}")
    full = json.loads(handle_line(oracle, json.dumps(step)))
    if full["code"] != "ok" or full["idempotent"] is not True or full["projection"] is None:
        raise AssertionError(f"the step after a brief one answered {full}")
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
