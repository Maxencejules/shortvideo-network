"""Transition fixtures for the protocol v0 state machine.

These cases are specification checks. They are not a four-validator devnet
and they do not approve DEC-001 through DEC-008.
"""

from __future__ import annotations

import copy
import hashlib

from spec_machine import (
    APP_HASH_EXCLUDED,
    AUDITOR,
    AUDITOR_KEY,
    AUTHORITY,
    BROADCASTER,
    CHAIN_TIME,
    DRAIN_DOMAIN,
    ENDPOINT,
    EVIDENCE_DOMAIN,
    HOSTING,
    MANIFEST,
    PACKAGE_BYTES,
    PROVIDER_ACCOUNT,
    REASON,
    RENEW_DOMAIN,
    ROTATE_DOMAIN,
    SERVICE,
    TERM_SECONDS,
    UINT64_MAX,
    accept,
    admit_change_payload,
    admit_provider,
    app_hash,
    apply,
    devnet_state,
    disable_change_payload,
    force_withdraw_payload,
    governed_limit_payload,
    intent,
    offer,
    production_state,
    proposal_step,
    pubkey_for,
    register_content,
    register_provider,
    signature,
    validator_power_payload,
)


# Pinned after the replay sequence below. Replaced once the tree is stable.
REPLAY_GOLDEN = "ffee724a8b7944512db064250664d46b7a519d2062134b5cdda9a03ca202437d"


def _play(state: dict, steps: list[dict]) -> str | None:
    for step in steps:
        expected = step.get("expect", "ok")
        result = apply(state, step)
        actual = "ok" if result["ok"] else str(result["error"])
        if actual != expected:
            return f"{step.get('op')} expected {expected} got {actual}"
    return None


def _seed(capacity: int = 1_000_000, package_bytes: int = PACKAGE_BYTES) -> dict:
    state = devnet_state()
    problem = _play(
        state,
        [
            register_provider(capacity=capacity),
            admit_provider(),
            register_content(package_bytes=package_bytes),
        ],
    )
    if problem:
        raise RuntimeError(problem)
    return state


def _active() -> dict:
    state = _seed()
    problem = _play(state, [offer(), accept()])
    if problem:
        raise RuntimeError(problem)
    return state


def _propose(proposal_id: str, value: int, limit_key: str = "max_evidence_batch", base: int = 1) -> dict:
    return proposal_step(proposal_id, limit_key, governed_limit_payload(limit_key, value), base=base)


def _council(proposal_id: str, parameter_key: str, payload: dict, execute_at: int = CHAIN_TIME + 10) -> list[dict]:
    return [
        proposal_step(proposal_id, parameter_key, payload),
        _approve(proposal_id, "seat-1"),
        _approve(proposal_id, "seat-2"),
        {"op": "CommitBlock", "header_time": execute_at},
        _execute(proposal_id),
    ]


def _approve(proposal_id: str, seat: str) -> dict:
    return {
        "op": "ApproveChange",
        "signer": seat,
        "business_id": f"yes-{proposal_id}-{seat}",
        "proposal_id": proposal_id,
    }


def _execute(proposal_id: str) -> dict:
    return {
        "op": "ExecuteChange",
        "signer": AUTHORITY,
        "business_id": f"exec-{proposal_id}",
        "proposal_id": proposal_id,
    }


def cases() -> list[tuple[str, str | None]]:
    found: list[tuple[str, str | None]] = []

    def check(case_id: str, problem: str | None) -> None:
        found.append((case_id, problem))

    state = devnet_state()
    long_id = "a" + ("b" * 63)
    longer_id = "a" + ("b" * 64)
    registered = register_provider(provider_id=long_id)
    registered["business_id"] = "reg-long"
    registered["signing_key_id"] = "key-long"
    admitted = admit_provider(provider_id=long_id)
    admitted["business_id"] = "admit-long"
    check("boundary-id-64", _play(state, [registered, admitted]))
    state = devnet_state()
    bad = register_provider(provider_id=longer_id)
    bad["expect"] = "ERROR_CODE_BOUNDS"
    check("boundary-id-65", _play(state, [bad]))

    state = devnet_state()
    missing = register_provider()
    del missing["business_id"]
    missing["expect"] = "ERROR_CODE_MALFORMED"
    check("malformed-missing-field", _play(state, [missing]))

    state = devnet_state()
    extra = register_provider()
    extra["host_observed_unix"] = CHAIN_TIME
    extra["expect"] = "ERROR_CODE_UNKNOWN_FIELD"
    check("unknown-field", _play(state, [extra]))

    state = _seed()
    check("duplicate-lease", _play(state, [offer(), offer("o2"), accept(), accept("o2", "l2", business_id="accept-l2") | {"expect": "ERROR_CODE_DUPLICATE_ID"}]))

    state = _seed()
    bad_accept = accept()
    bad_accept["provider_acceptance"] = signature(HOSTING, "key-p1")
    bad_accept["expect"] = "ERROR_CODE_WRONG_DOMAIN"
    check("wrong-domain", _play(state, [offer(), bad_accept]))

    state = _seed()
    check(
        "expired-offer",
        _play(
            state,
            [
                offer(),
                {"op": "CommitBlock", "header_time": CHAIN_TIME + 500},
                accept() | {"expect": "ERROR_CODE_EXPIRED"},
            ],
        ),
    )

    state = _seed()
    outsider = accept()
    outsider["provider_acceptance"] = signature("SVN-PROVIDER-ACCEPT-1", "key-other")
    outsider["expect"] = "ERROR_CODE_UNAUTHORIZED"
    check("unauthorized-signer", _play(state, [offer(), outsider]))

    state = _seed()
    forged = accept(valid=False)
    forged["expect"] = "ERROR_CODE_UNAUTHORIZED"
    check("forged-signature", _play(state, [offer(), forged]))

    state = _seed()
    first = accept()
    retry = accept()
    retry["tx_hash"] = "tx-retry"
    check("replay-same-business", _play(state, [offer(), first, retry]))
    if found[-1][1] is None and state["providers"]["p1"]["reserved_bytes"] != PACKAGE_BYTES:
        found[-1] = (found[-1][0], "reservation changed on retry")

    state = _seed()
    other = accept(lease_id="l9", business_id="accept-l1")
    other["expect"] = "ERROR_CODE_REPLAY"
    check("replay-different-payload", _play(state, [offer(), accept(), other]))

    state = devnet_state()
    check(
        "stall-one-validator",
        _play(state, [{"op": "SetDown", "validators": ["v1"]}, {"op": "CommitBlock", "header_time": CHAIN_TIME}]),
    )
    if found[-1][1] is None and state["height"] != 2:
        found[-1] = (found[-1][0], "height did not advance")

    state = devnet_state()
    stalled = _seed()
    check(
        "stall-two-validators",
        _play(
            stalled,
            [
                offer(),
                {"op": "SetDown", "validators": ["v1", "v2"]},
                {"op": "CommitBlock", "header_time": CHAIN_TIME + 1, "expect": "ERROR_CODE_QUORUM"},
                {"op": "Submit", "request_id": "req-1", "tx": accept(), "expect": "ERROR_CODE_QUORUM"},
            ],
        ),
    )
    if found[-1][1] is None:
        if stalled["height"] != 1 or stalled["leases"] or stalled["pending"]["req-1"]["status"] != "PENDING_STALL":
            found[-1] = (found[-1][0], "stall presented a commit or dropped the pending request")

    good = hashlib.sha256(b"segment").hexdigest()
    check(
        "corrupt-manifest",
        _play(devnet_state(), [{"op": "CheckReplica", "segment_bytes_hex": "00", "expected_sha256": good, "expect": "ERROR_CODE_INTEGRITY"}]),
    )
    check(
        "replica-match",
        _play(devnet_state(), [{"op": "CheckReplica", "segment_bytes_hex": "7365676d656e74", "expected_sha256": good}]),
    )

    state = _active()
    withdrawn = {
        "op": "WithdrawContent",
        "signer": SERVICE,
        "business_id": "withdraw-c1",
        "content_id": "c1",
        "content_version": 1,
        "reason_digest": REASON,
        "hosting_intent": intent(),
    }
    check("withdrawn-blocks-offer", _play(state, [withdrawn, offer("o9") | {"expect": "ERROR_CODE_WITHDRAWN"}]))
    state = _seed()
    revival = register_content()
    revival["business_id"] = "content-c1-again"
    revival["expect"] = "ERROR_CODE_DUPLICATE_ID"
    check(
        "withdrawn-blocks-revival",
        _play(
            state,
            [
                {
                    "op": "WithdrawContent",
                    "signer": SERVICE,
                    "business_id": "withdraw-c1b",
                    "content_id": "c1",
                    "content_version": 1,
                    "reason_digest": REASON,
                    "hosting_intent": intent(),
                },
                revival,
            ],
        ),
    )
    if found[-1][1] is None and state["contents"]["c1"]["tombstone"]["history_erased"] is not False:
        found[-1] = (found[-1][0], "tombstone erased history")

    state = _active()
    reserved = state["providers"]["p1"]["reserved_bytes"]
    check(
        "underflow-double-end",
        _play(
            state,
            [
                {
                    "op": "EndLease",
                    "signer": BROADCASTER,
                    "business_id": "end-l1",
                    "lease_id": "l1",
                    "authority_end": False,
                    "provider_signature": signature("SVN-PROVIDER-END-1", "key-p1"),
                },
                {
                    "op": "EndLease",
                    "signer": BROADCASTER,
                    "business_id": "end-l1-again",
                    "lease_id": "l1",
                    "authority_end": False,
                    "provider_signature": signature("SVN-PROVIDER-END-1", "key-p1"),
                    "expect": "ERROR_CODE_UNDERFLOW",
                },
            ],
        ),
    )
    if found[-1][1] is None and state["providers"]["p1"]["reserved_bytes"] != 0:
        found[-1] = (found[-1][0], f"reserved {state['providers']['p1']['reserved_bytes']} after one release of {reserved}")

    state = _seed()
    check(
        "overflow-reserve",
        _play(
            state,
            [
                offer(package_bytes=PACKAGE_BYTES),
                {
                    "op": "FixtureSetCounters",
                    "fixture_hook": "NOT_A_TRANSACTION",
                    "provider_id": "p1",
                    "reserved_bytes": UINT64_MAX - 5,
                    "admitted_capacity_bytes": UINT64_MAX,
                },
                accept() | {"expect": "ERROR_CODE_OVERFLOW"},
            ],
        ),
    )

    state = _active()
    check(
        "stale-term-version",
        _play(
            state,
            [
                {
                    "op": "RenewLease",
                    "signer": BROADCASTER,
                    "business_id": "renew-stale",
                    "lease_id": "l1",
                    "term_version": 1,
                    "deadline_unix_seconds": CHAIN_TIME + TERM_SECONDS + TERM_SECONDS,
                    "package_bytes": PACKAGE_BYTES,
                    "manifest_digest": MANIFEST,
                    "provider_signature": signature(RENEW_DOMAIN, "key-p1"),
                    "expect": "ERROR_CODE_STALE_VERSION",
                }
            ],
        ),
    )

    state = devnet_state()
    check(
        "conflict-governance",
        _play(
            state,
            [
                _propose("pa", 1),
                _propose("pb", 2),
                _approve("pa", "seat-1"),
                _approve("pa", "seat-2"),
                {"op": "CommitBlock", "header_time": CHAIN_TIME + 10},
                _execute("pa"),
                _execute("pb") | {"expect": "ERROR_CODE_CONFLICT"},
            ],
        ),
    )
    if found[-1][1] is None and state["policy_version"] != 2:
        found[-1] = (found[-1][0], "policy version moved twice")
    if found[-1][1] is None and any(row["approved"] for row in state["decisions"].values()):
        found[-1] = (found[-1][0], "a decision was approved")

    state = _active()
    check(
        "evidence-expired-cannot-renew",
        _play(
            state,
            [
                {
                    "op": "SubmitAuditBatch",
                    "signer": AUDITOR,
                    "business_id": "audit-1",
                    "batch_id": "batch-1",
                    "epoch": 1,
                    "evidence_id": "ev-1",
                    "kind": "AUDITOR_ATTESTATION",
                    "lease_id": "l1",
                    "content_id": "c1",
                    "content_version": 1,
                    "expiry_unix_seconds": CHAIN_TIME + 10,
                    "attestation": True,
                    "auditor_signature": signature(EVIDENCE_DOMAIN, AUDITOR_KEY),
                },
                {"op": "CommitBlock", "header_time": CHAIN_TIME + 10},
                {"op": "ExpireDue", "limit": 8},
                {
                    "op": "RenewLease",
                    "signer": BROADCASTER,
                    "business_id": "renew-ev",
                    "lease_id": "l1",
                    "term_version": 2,
                    "deadline_unix_seconds": CHAIN_TIME + TERM_SECONDS + TERM_SECONDS,
                    "package_bytes": PACKAGE_BYTES,
                    "manifest_digest": MANIFEST,
                    "provider_signature": signature(RENEW_DOMAIN, "key-p1"),
                    "evidence_id": "ev-1",
                    "expect": "ERROR_CODE_EXPIRED",
                },
            ],
        ),
    )

    state = _active()
    deadline = state["leases"]["l1"]["deadline_unix_seconds"]
    check(
        "evidence-does-not-extend-lease",
        _play(
            state,
            [
                {
                    "op": "SubmitAuditBatch",
                    "signer": AUDITOR,
                    "business_id": "audit-2",
                    "batch_id": "batch-2",
                    "epoch": 1,
                    "evidence_id": "ev-2",
                    "kind": "AUDITOR_ATTESTATION",
                    "lease_id": "l1",
                    "content_id": "c1",
                    "content_version": 1,
                    "expiry_unix_seconds": CHAIN_TIME + 1000,
                    "attestation": True,
                    "auditor_signature": signature(EVIDENCE_DOMAIN, AUDITOR_KEY),
                },
                {
                    "op": "RenewLease",
                    "signer": BROADCASTER,
                    "business_id": "renew-ev2",
                    "lease_id": "l1",
                    "term_version": 2,
                    "deadline_unix_seconds": deadline + TERM_SECONDS,
                    "package_bytes": PACKAGE_BYTES,
                    "manifest_digest": MANIFEST,
                    "provider_signature": signature(RENEW_DOMAIN, "key-p1"),
                    "evidence_id": "ev-2",
                    "expect": "ERROR_CODE_FORBIDDEN",
                },
            ],
        ),
    )
    if found[-1][1] is None and state["leases"]["l1"]["deadline_unix_seconds"] != deadline:
        found[-1] = (found[-1][0], "evidence changed the deadline")

    check(
        "decision-blocked-without-devnet",
        _play(production_state(), [register_content() | {"expect": "ERROR_CODE_DECISION_BLOCKED"}]),
    )
    state = devnet_state()
    check(
        "devnet-does-not-approve-decision",
        _play(state, [{"op": "ApproveDecision", "decision_id": "DEC-001", "expect": "ERROR_CODE_DECISION_BLOCKED"}]),
    )
    if found[-1][1] is None and state["decisions"]["DEC-001"]["approved"]:
        found[-1] = (found[-1][0], "DEC-001 approved")

    state = _seed()
    clock = offer()
    clock["host_observed_unix"] = 1
    clock["expect"] = "ERROR_CODE_UNKNOWN_FIELD"
    check("no-local-clock-field", _play(state, [clock]))

    check("zero-fee", _play(devnet_state(), [{"op": "CheckFee", "amount": 0, "denom": "", "gas_limit": 1}]))
    check(
        "nonzero-fee",
        _play(devnet_state(), [{"op": "CheckFee", "amount": 1, "denom": "token", "gas_limit": 1, "expect": "ERROR_CODE_FEE_REJECTED"}]),
    )
    state = devnet_state()
    before = dict(state["validators"])
    check("reject-msg-create-validator", _play(state, [{"op": "MsgCreateValidator", "expect": "ERROR_CODE_FORBIDDEN"}]))
    if found[-1][1] is None and state["validators"] != before:
        found[-1] = (found[-1][0], "validator set changed")
    check("reject-mint", _play(devnet_state(), [{"op": "EnableMint", "expect": "ERROR_CODE_FORBIDDEN"}]))

    state = _active()
    check(
        "publication-separate",
        _play(state, [{"op": "SetPublication", "content_id": "c1", "status": "PUBLICATION_STATUS_APPROVED"}]),
    )
    if found[-1][1] is None and state["contents"]["c1"]["state"] != "PENDING":
        found[-1] = (found[-1][0], "publication wrote a chain content state")
    if found[-1][1] is None and state["leases"]["l1"]["state"] != "ACTIVE":
        found[-1] = (found[-1][0], "publication changed the lease")

    state = _active()
    check(
        "reachability-not-lease-state",
        _play(
            state,
            [
                {"op": "SetReachability", "provider_id": "p1", "lease_id": "l1", "status": "REACHABILITY_DEGRADED"},
                {"op": "RejectLeaseStateName", "state_name": "LEASE_STATE_AT_RISK", "expect": "ERROR_CODE_MALFORMED"},
            ],
        ),
    )
    if found[-1][1] is None and state["leases"]["l1"]["state"] != "ACTIVE":
        found[-1] = (found[-1][0], "reachability changed the lease")

    check(
        "capacity-exceeded",
        _play(_seed(capacity=500), [offer() | {"expect": "ERROR_CODE_CAPACITY"}]),
    )

    state = _active()
    events_before = len(state["events"])
    check("idempotent-retry-no-double-reserve", _play(state, [accept()]))
    if found[-1][1] is None and (
        state["providers"]["p1"]["reserved_bytes"] != PACKAGE_BYTES or len(state["events"]) != events_before
    ):
        found[-1] = (found[-1][0], "retry reserved twice or emitted another event")

    state = _seed()
    check(
        "bounded-expiry",
        _play(
            state,
            [
                offer("o1", expiry=CHAIN_TIME + 5),
                offer("o2", expiry=CHAIN_TIME + 5),
                {"op": "CommitBlock", "header_time": CHAIN_TIME + 5},
                {"op": "ExpireDue", "limit": 1},
            ],
        ),
    )
    if found[-1][1] is None and len(state["offers"]) != 1:
        found[-1] = (found[-1][0], f"expiry processed {2 - len(state['offers'])} offers")

    state = _active()
    check(
        "drain-stops-new-offers",
        _play(
            state,
            [
                {
                    "op": "DrainProvider",
                    "signer": PROVIDER_ACCOUNT,
                    "business_id": "drain-p1",
                    "provider_id": "p1",
                    "provider_signature": signature(DRAIN_DOMAIN, "key-p1"),
                },
                offer("o8") | {"expect": "ERROR_CODE_FORBIDDEN"},
            ],
        ),
    )
    if found[-1][1] is None and state["leases"]["l1"]["reservation_held"] is not True:
        found[-1] = (found[-1][0], "drain released an accepted reservation")

    state = _seed()
    check(
        "rotate-key-stale-generation",
        _play(
            state,
            [
                {
                    "op": "RotateProviderKey",
                    "signer": PROVIDER_ACCOUNT,
                    "business_id": "rot-1",
                    "provider_id": "p1",
                    "new_signing_key_id": "key-p1b",
                    "new_signing_pubkey": pubkey_for("p1b"),
                    "expected_generation": 9,
                    "current_key_signature": signature(ROTATE_DOMAIN, "key-p1"),
                    "expect": "ERROR_CODE_STALE_VERSION",
                }
            ],
        ),
    )

    check(
        "storage-proof-rejected",
        _play(
            _active(),
            [
                {
                    "op": "SubmitAuditBatch",
                    "signer": AUDITOR,
                    "business_id": "audit-proof",
                    "batch_id": "batch-proof",
                    "epoch": 1,
                    "evidence_id": "ev-proof",
                    "kind": "STORAGE_PROOF",
                    "lease_id": "l1",
                    "content_id": "c1",
                    "content_version": 1,
                    "expiry_unix_seconds": CHAIN_TIME + 100,
                    "attestation": True,
                    "auditor_signature": signature(EVIDENCE_DOMAIN, AUDITOR_KEY),
                    "expect": "ERROR_CODE_MALFORMED",
                }
            ],
        ),
    )
    hosted = register_content()
    hosted["business_id"] = "content-host-role"
    hosted["content_id"] = "c2"
    hosted["hosting_intent"] = intent(role_name="ACCOUNT_HOST_DELEGATE")
    hosted["expect"] = "ERROR_CODE_FORBIDDEN"
    check("host-custody-not-assumed", _play(devnet_state(), [hosted]))

    state = _active()
    renewed_deadline = state["leases"]["l1"]["deadline_unix_seconds"] + TERM_SECONDS
    reserved_before = state["providers"]["p1"]["reserved_bytes"]
    check(
        "renew-succeeds",
        _play(
            state,
            [
                {
                    "op": "RenewLease",
                    "signer": BROADCASTER,
                    "business_id": "renew-ok",
                    "lease_id": "l1",
                    "term_version": 2,
                    "deadline_unix_seconds": renewed_deadline,
                    "package_bytes": PACKAGE_BYTES,
                    "manifest_digest": MANIFEST,
                    "provider_signature": signature(RENEW_DOMAIN, "key-p1"),
                }
            ],
        ),
    )
    if found[-1][1] is None and (
        state["leases"]["l1"]["state"] != "RENEWED" or state["providers"]["p1"]["reserved_bytes"] != reserved_before
    ):
        found[-1] = (found[-1][0], "renewal did not keep the reservation")

    state = _active()
    check(
        "renew-changed-bytes",
        _play(
            state,
            [
                {
                    "op": "RenewLease",
                    "signer": BROADCASTER,
                    "business_id": "renew-bytes",
                    "lease_id": "l1",
                    "term_version": 2,
                    "deadline_unix_seconds": CHAIN_TIME + TERM_SECONDS + TERM_SECONDS,
                    "package_bytes": PACKAGE_BYTES + 1,
                    "manifest_digest": MANIFEST,
                    "provider_signature": signature(RENEW_DOMAIN, "key-p1"),
                    "expect": "ERROR_CODE_CONFLICT",
                }
            ],
        ),
    )

    state = _active()
    check(
        "renew-manifest-mismatch",
        _play(
            state,
            [
                {
                    "op": "RenewLease",
                    "signer": BROADCASTER,
                    "business_id": "renew-digest",
                    "lease_id": "l1",
                    "term_version": 2,
                    "deadline_unix_seconds": CHAIN_TIME + TERM_SECONDS + TERM_SECONDS,
                    "package_bytes": PACKAGE_BYTES,
                    "manifest_digest": hashlib.sha256(b"svn-manifest-v2").hexdigest(),
                    "provider_signature": signature(RENEW_DOMAIN, "key-p1"),
                    "expect": "ERROR_CODE_CONFLICT",
                }
            ],
        ),
    )
    if found[-1][1] is None and state["leases"]["l1"]["state"] == "RENEWED":
        found[-1] = (found[-1][0], "digest mismatch renewed the lease")

    state = _seed()
    check(
        "update-provider",
        _play(
            state,
            [
                {
                    "op": "UpdateProvider",
                    "signer": PROVIDER_ACCOUNT,
                    "business_id": "upd-p1",
                    "provider_id": "p1",
                    "declared_capacity_bytes": 2_000_000,
                    "endpoint_manifest_digest": hashlib.sha256(b"svn-endpoint-v2").hexdigest(),
                }
            ],
        ),
    )
    updated_digest = hashlib.sha256(b"svn-endpoint-v2").hexdigest()
    if found[-1][1] is None and state["providers"]["p1"]["admitted_capacity_bytes"] != 2_000_000:
        found[-1] = (found[-1][0], "capacity was not updated")
    if found[-1][1] is None and state["providers"]["p1"]["endpoint_manifest_digest"] != updated_digest:
        found[-1] = (found[-1][0], "endpoint digest was not stored")

    state = _active()
    check(
        "update-provider-below-reserved",
        _play(
            state,
            [
                {
                    "op": "UpdateProvider",
                    "signer": PROVIDER_ACCOUNT,
                    "business_id": "upd-low",
                    "provider_id": "p1",
                    "declared_capacity_bytes": 1,
                    "endpoint_manifest_digest": ENDPOINT,
                    "expect": "ERROR_CODE_CAPACITY",
                }
            ],
        ),
    )

    check(
        "admit-blocked-by-dec-002",
        _play(
            production_state(),
            [register_provider(), admit_provider() | {"expect": "ERROR_CODE_DECISION_BLOCKED"}],
        ),
    )
    state = _seed()
    # _seed already admitted p1. Register a second id and admit with the wrong role.
    second = register_provider(provider_id="p2")
    bad_admit = admit_provider(provider_id="p2")
    bad_admit["signer"] = SERVICE
    bad_admit["expect"] = "ERROR_CODE_FORBIDDEN"
    check("admit-non-authority", _play(devnet_state(), [second, bad_admit]))

    state = _seed()
    first_withdraw = {
        "op": "WithdrawContent",
        "signer": SERVICE,
        "business_id": "withdraw-once",
        "content_id": "c1",
        "content_version": 1,
        "reason_digest": REASON,
        "hosting_intent": intent(),
    }
    second_withdraw = dict(first_withdraw)
    second_withdraw["business_id"] = "withdraw-twice"
    second_withdraw["expect"] = "ERROR_CODE_WITHDRAWN"
    check("second-withdraw", _play(state, [first_withdraw, second_withdraw]))

    state = devnet_state()
    check(
        "approve-same-seat-replay",
        _play(
            state,
            [
                _propose("ps", 3),
                _approve("ps", "seat-1"),
                _approve("ps", "seat-1") | {"business_id": "yes-ps-seat-1-again", "expect": "ERROR_CODE_REPLAY"},
            ],
        ),
    )

    state = devnet_state()
    stale = _propose("stale", 4, base=9)
    stale["expect"] = "ERROR_CODE_STALE_VERSION"
    named = _propose("named", 4, limit_key="dec-008")
    named["expect"] = "ERROR_CODE_FORBIDDEN"
    check("propose-stale-base", _play(state, [stale]))
    check("propose-dec-named-key", _play(devnet_state(), [named]))

    state = devnet_state()
    check(
        "execute-before-delay",
        _play(
            state,
            [
                _propose("early", 5),
                _approve("early", "seat-1"),
                _approve("early", "seat-2"),
                _execute("early") | {"expect": "ERROR_CODE_OUT_OF_RANGE"},
            ],
        ),
    )

    state = _active()
    check(
        "authority-end-lease",
        _play(
            state,
            [
                {
                    "op": "EndLease",
                    "signer": AUTHORITY,
                    "business_id": "end-auth",
                    "lease_id": "l1",
                    "authority_end": True,
                }
            ],
        ),
    )
    if found[-1][1] is None and state["providers"]["p1"]["reserved_bytes"] != 0:
        found[-1] = (found[-1][0], "authority end did not release")

    state = _seed()
    check(
        "rotate-then-old-key-fails",
        _play(
            state,
            [
                {
                    "op": "RotateProviderKey",
                    "signer": PROVIDER_ACCOUNT,
                    "business_id": "rot-ok",
                    "provider_id": "p1",
                    "new_signing_key_id": "key-p1b",
                    "new_signing_pubkey": pubkey_for("p1b"),
                    "expected_generation": 1,
                    "current_key_signature": signature(ROTATE_DOMAIN, "key-p1"),
                },
                offer(),
                accept(key_id="key-p1") | {"expect": "ERROR_CODE_UNAUTHORIZED"},
                accept(lease_id="l2", business_id="accept-l2", key_id="key-p1b"),
            ],
        ),
    )
    if found[-1][1] is None and state["providers"]["p1"]["signing_pubkey"] != pubkey_for("p1b"):
        found[-1] = (found[-1][0], "pubkey was not rotated")

    state = _active()
    check(
        "provider-disabled",
        _play(
            state,
            [
                {"op": "DisableProvider", "signer": AUTHORITY, "business_id": "dis-p1", "provider_id": "p1"},
                offer("o-disabled") | {"expect": "ERROR_CODE_FORBIDDEN"},
            ],
        ),
    )
    if found[-1][1] is None and (
        state["providers"]["p1"]["state"] != "DISABLED" or state["leases"]["l1"]["reservation_held"] is not True
    ):
        found[-1] = (found[-1][0], "disablement changed the accepted reservation")

    state = devnet_state()
    expiring = _propose("exp", 6)
    expiring["expiry_unix_seconds"] = CHAIN_TIME + 20
    check(
        "proposal-expired",
        _play(
            state,
            [
                expiring,
                {"op": "CommitBlock", "header_time": CHAIN_TIME + 20},
                _execute("exp") | {"expect": "ERROR_CODE_EXPIRED"},
            ],
        ),
    )
    if found[-1][1] is None and state["proposals"]["exp"]["state"] != "EXPIRED":
        found[-1] = (found[-1][0], "proposal was not marked expired")

    state = devnet_state()
    state["validators"] = {"v1": 1}
    zero = validator_power_payload("v1", 0, {"height": "9"})
    check(
        "proposal-rejected",
        _play(
            state,
            _council("zero", "validator-v1", zero)[:-1]
            + [_execute("zero") | {"expect": "ERROR_CODE_FORBIDDEN"}],
        ),
    )
    if found[-1][1] is None and (state["proposals"]["zero"]["state"] != "REJECTED" or state["validators"] != {"v1": 1}):
        found[-1] = (found[-1][0], "zero-power change was applied")

    state = devnet_state()
    added = validator_power_payload("v5", 1, {"height": "3"})
    check(
        "validator-power-applied",
        _play(state, _council("addv5", "validator-v5", added)),
    )
    if found[-1][1] is None and (state["validators"].get("v5") is not None or len(state["pending_power"]) != 1):
        found[-1] = (found[-1][0], "validator power was applied inside ExecuteChange")
    if found[-1][1] is None:
        activated = _play(state, [{"op": "CommitBlock", "header_time": CHAIN_TIME + 11}])
        if activated:
            found[-1] = (found[-1][0], activated)
        elif state["validators"].get("v5") != 1 or state["pending_power"] or state["validator_reasons"].get("v5") != REASON:
            found[-1] = (found[-1][0], "validator power was not applied at the activation height")

    state = devnet_state()
    timed = validator_power_payload("v6", 1, {"unixSeconds": str(CHAIN_TIME + 50)})
    check(
        "validator-power-time-applied",
        _play(state, _council("addv6", "validator-v6", timed)),
    )
    if found[-1][1] is None and state["validators"].get("v6") is not None:
        found[-1] = (found[-1][0], "time activation was applied immediately")
    if found[-1][1] is None:
        activated = _play(state, [{"op": "CommitBlock", "header_time": CHAIN_TIME + 50}])
        if activated:
            found[-1] = (found[-1][0], activated)
        elif state["validators"].get("v6") != 1 or state["pending_power"]:
            found[-1] = (found[-1][0], "validator power was not applied at the activation time")

    immediate = validator_power_payload("v5", 1, {"height": "2"})
    check(
        "validator-activation-not-immediate",
        _play(
            devnet_state(),
            _council("now", "validator-v5", immediate)[:-1]
            + [_execute("now") | {"expect": "ERROR_CODE_OUT_OF_RANGE"}],
        ),
    )

    missing_reason = {
        "validatorPower": {
            "activation": {"height": "9"},
            "consensusPubkey": validator_power_payload("v5", 1, {"height": "9"})["validatorPower"]["consensusPubkey"],
            "nodeId": "v5",
            "power": "1",
        }
    }
    missing_step = proposal_step("noreason", "validator-v5", missing_reason)
    missing_step["expect"] = "ERROR_CODE_MALFORMED"
    check("validator-power-missing-reason", _play(devnet_state(), [missing_step]))

    duplicate = validator_power_payload("v5", 1, {"height": "9"}, pubkey_hex=pubkey_for("v1"))
    duplicate_state = devnet_state()
    check(
        "duplicate-validator-pubkey",
        _play(
            duplicate_state,
            _council("dup", "validator-v5", duplicate)[:-1]
            + [_execute("dup") | {"expect": "ERROR_CODE_DUPLICATE_ID"}],
        ),
    )
    if found[-1][1] is None and (duplicate_state["validators"].get("v5") is not None or duplicate_state["pending_power"]):
        found[-1] = (found[-1][0], "duplicate validator key was scheduled")

    state = _active()
    check(
        "evidence-rejected",
        _play(
            state,
            [
                {
                    "op": "SubmitAuditBatch",
                    "signer": AUDITOR,
                    "business_id": "audit-rej",
                    "batch_id": "batch-rej",
                    "epoch": 1,
                    "evidence_id": "ev-rej",
                    "kind": "AUDITOR_ATTESTATION",
                    "lease_id": "l1",
                    "content_id": "c1",
                    "content_version": 1,
                    "expiry_unix_seconds": CHAIN_TIME + 100,
                    "attestation": True,
                    "reject": True,
                    "auditor_signature": signature(EVIDENCE_DOMAIN, AUDITOR_KEY),
                }
            ],
        ),
    )
    if found[-1][1] is None and state["evidence"]["ev-rej"]["state"] != "REJECTED":
        found[-1] = (found[-1][0], "evidence was not rejected")
    if found[-1][1] is None and state["leases"]["l1"]["state"] != "ACTIVE":
        found[-1] = (found[-1][0], "rejected evidence changed the lease")

    check(
        "force-withdraw",
        _play(
            _seed(),
            [
                {
                    "op": "ForceWithdraw",
                    "signer": AUTHORITY,
                    "business_id": "force-c1",
                    "content_id": "c1",
                    "content_version": 1,
                    "reason_digest": REASON,
                }
            ],
        ),
    )
    # The play above discards state. Repeat into a named state.
    forced = _seed()
    problem = _play(
        forced,
        [
            {
                "op": "ForceWithdraw",
                "signer": AUTHORITY,
                "business_id": "force-c1b",
                "content_id": "c1",
                "content_version": 1,
                "reason_digest": REASON,
            }
        ],
    )
    if problem:
        found[-1] = ("force-withdraw", problem)
    elif forced["contents"]["c1"]["tombstone"]["history_erased"] or forced["contents"]["c1"]["removal_promise_approved"]:
        found[-1] = ("force-withdraw", "force withdraw approved a removal promise or erased history")

    bad_force = {
        "op": "ForceWithdraw",
        "signer": SERVICE,
        "business_id": "force-nope",
        "content_id": "c1",
        "content_version": 1,
        "reason_digest": REASON,
        "expect": "ERROR_CODE_FORBIDDEN",
    }
    check("force-withdraw-non-authority", _play(_seed(), [bad_force]))

    over_cap = devnet_state()
    check(
        "governed-limit-over-key-cap",
        _play(
            over_cap,
            _council("over", "max_evidence_batch", governed_limit_payload("max_evidence_batch", 33))[:-1]
            + [_execute("over") | {"expect": "ERROR_CODE_BOUNDS"}],
        ),
    )
    if found[-1][1] is None and over_cap["governed_limits"]:
        found[-1] = (found[-1][0], "over-cap evidence batch was stored")

    limited = devnet_state()
    check(
        "governed-limit-read",
        _play(limited, _council("cap", "max_evidence_batch", governed_limit_payload("max_evidence_batch", 2))),
    )
    if found[-1][1] is None and limited["governed_limits"].get("max_evidence_batch") != 2:
        found[-1] = (found[-1][0], "governed evidence cap was not stored")
    if found[-1][1] is None:
        too_many = _play(
            limited,
            [
                {
                    "op": "SubmitAuditBatch",
                    "signer": AUDITOR,
                    "business_id": "audit-cap",
                    "batch_id": "batch-cap",
                    "epoch": 1,
                    "evidence_id": "ev-cap",
                    "kind": "AUDITOR_ATTESTATION",
                    "lease_id": "l1",
                    "content_id": "c1",
                    "content_version": 1,
                    "expiry_unix_seconds": CHAIN_TIME + 100,
                    "attestation": True,
                    "item_count": 3,
                    "auditor_signature": signature(EVIDENCE_DOMAIN, AUDITOR_KEY),
                    "expect": "ERROR_CODE_BOUNDS",
                }
            ],
        )
        if too_many:
            found[-1] = (found[-1][0], too_many)

    blocked_admit = devnet_state()
    del blocked_admit["devnet_tests"]["DEC-002"]
    check(
        "admit-payload-blocked-by-dec-002",
        _play(
            blocked_admit,
            [register_provider()]
            + _council("padmit", "admit-p1", admit_change_payload("p1"))[:-1]
            + [_execute("padmit") | {"expect": "ERROR_CODE_DECISION_BLOCKED"}],
        ),
    )
    if found[-1][1] is None and blocked_admit["providers"]["p1"]["state"] != "APPLIED":
        found[-1] = (found[-1][0], "admit payload skipped DEC-002")

    admitted = devnet_state()
    check(
        "execute-admit-provider",
        _play(admitted, [register_provider(), *_council("eadmit", "admit-p1", admit_change_payload("p1"))]),
    )
    if found[-1][1] is None and admitted["providers"]["p1"]["state"] != "ADMITTED":
        found[-1] = (found[-1][0], "ExecuteChange did not admit the provider")

    disabled = _active()
    check(
        "execute-disable-provider",
        _play(disabled, _council("edisable", "disable-p1", disable_change_payload("p1"))),
    )
    if found[-1][1] is None and (
        disabled["providers"]["p1"]["state"] != "DISABLED" or disabled["leases"]["l1"]["reservation_held"] is not True
    ):
        found[-1] = (found[-1][0], "ExecuteChange did not disable the provider")

    withdrawn = _seed()
    check(
        "execute-force-withdraw",
        _play(withdrawn, _council("eforce", "withdraw-c1", force_withdraw_payload("c1"))),
    )
    if found[-1][1] is None and (
        withdrawn["contents"]["c1"]["state"] != "WITHDRAWN"
        or withdrawn["contents"]["c1"]["tombstone"]["history_erased"]
        or withdrawn["contents"]["c1"]["removal_promise_approved"]
    ):
        found[-1] = (found[-1][0], "ExecuteChange force-withdraw did not keep the tombstone rules")

    expiring_lease = _active()
    deadline = expiring_lease["leases"]["l1"]["deadline_unix_seconds"]
    check(
        "lease-expired",
        _play(
            expiring_lease,
            [
                {"op": "CommitBlock", "header_time": deadline},
                {"op": "ExpireDue", "limit": 8},
            ],
        ),
    )
    if found[-1][1] is None and (
        expiring_lease["leases"]["l1"]["state"] != "EXPIRED"
        or expiring_lease["leases"]["l1"]["reservation_held"]
        or expiring_lease["providers"]["p1"]["reserved_bytes"] != 0
    ):
        found[-1] = (found[-1][0], "expiry did not release the reservation")

    left = devnet_state()
    right = devnet_state()
    steps = [register_provider(), admit_provider(), register_content(), offer(), accept()]
    left_problem = _play(left, steps)
    right_problem = _play(right, steps)
    replay_hash = app_hash(left) if left_problem is None else ""
    replay_problem = left_problem or right_problem
    if replay_problem is None and replay_hash != app_hash(right):
        replay_problem = "hashes diverged"
    if replay_problem is None and replay_hash != REPLAY_GOLDEN:
        replay_problem = f"golden mismatch {replay_hash}"
    if replay_problem is None:
        leaked = copy.deepcopy(left)
        leaked["publication"]["c1"] = "PUBLICATION_STATUS_APPROVED"
        leaked["reachability"]["p1"] = "REACHABILITY_UNREACHABLE"
        if app_hash(leaked) != replay_hash:
            replay_problem = "off-chain records are inside the app hash"
        probes = {
            "leases": lambda clone: clone["leases"].clear(),
            "contents": lambda clone: clone["contents"].clear(),
            "offers": lambda clone: clone["offers"].__setitem__("probe", {"offer_id": "probe"}),
            "evidence": lambda clone: clone["evidence"].__setitem__("probe", {"state": "RECORDED"}),
            "proposals": lambda clone: clone["proposals"].__setitem__("probe", {"state": "OPEN"}),
            "tombstone": lambda clone: clone["contents"]["c1"].__setitem__("tombstone", {"history_erased": False}),
            "validator_pubkeys": lambda clone: clone["validator_pubkeys"].__setitem__("v1", "ab" * 32),
            "signing_pubkey": lambda clone: clone["providers"]["p1"].__setitem__("signing_pubkey", "cd" * 32),
            "pending_power": lambda clone: clone["pending_power"].append({"proposal_id": "probe"}),
            "governed_limits": lambda clone: clone["governed_limits"].__setitem__("max_page_limit", 1),
            "pair_index": lambda clone: clone["pair_index"].clear(),
        }
        for label, mutate in probes.items():
            clone = copy.deepcopy(left)
            mutate(clone)
            if app_hash(clone) == replay_hash:
                replay_problem = f"{label} is outside the app hash"
                break
        if "leases" in APP_HASH_EXCLUDED or "publication" not in APP_HASH_EXCLUDED:
            replay_problem = "app hash exclusion list is wrong"
    check("replay-app-hash", replay_problem)

    hosts = devnet_state()
    resilience_steps = []
    for name in ("p1", "p2", "p3", "p4"):
        resilience_steps.append(register_provider(provider_id=name))
        resilience_steps.append(admit_provider(provider_id=name))
    resilience_steps.append(register_content())
    for name, offer_id, lease_id in (("p1", "o1", "l1"), ("p2", "o2", "l2"), ("p3", "o3", "l3")):
        resilience_steps.append(offer(offer_id, provider_id=name))
        resilience_steps.append(accept(offer_id, lease_id, business_id=f"accept-{lease_id}", key_id=f"key-{name}"))
    resilience_steps.append(
        {"op": "SetReachability", "provider_id": "p1", "lease_id": "l1", "status": "REACHABILITY_UNREACHABLE"}
    )
    resilience_steps.append(offer("o4", provider_id="p4"))
    resilience_steps.append(accept("o4", "l4", business_id="accept-l4", key_id="key-p4"))
    check("hosting-resilience", _play(hosts, resilience_steps))
    if found[-1][1] is None:
        lost = []
        if hosts["leases"]["l1"]["reservation_held"] is not True:
            lost.append("l1")
        if hosts["leases"]["l2"]["reservation_held"] is not True:
            lost.append("l2")
        if hosts["leases"]["l3"]["reservation_held"] is not True:
            lost.append("l3")
        if hosts["leases"]["l4"]["reservation_held"] is not True:
            lost.append("l4")
        if hosts["leases"]["l4"]["state"] != "ACTIVE":
            lost.append("l4-state")
        if hosts["reachability"]["p1"] != "REACHABILITY_UNREACHABLE":
            lost.append("reachability")
        if "origin" in hosts["providers"]:
            lost.append("origin")
        if lost:
            found[-1] = (found[-1][0], "hosting-resilience lost " + ",".join(lost))

    bad_digest = proposal_step("baddigest", "max_evidence_batch", governed_limit_payload("max_evidence_batch", 4))
    bad_digest["payload_digest"] = "0" * 64
    bad_digest["expect"] = "ERROR_CODE_INTEGRITY"
    check("wrong-payload-digest", _play(devnet_state(), [bad_digest]))

    explicit_zero = validator_power_payload("v5", 1, {"height": "9"})
    explicit_zero["validatorPower"]["power"] = "0"
    explicit_step = proposal_step("explicit0", "validator-v5", explicit_zero)
    explicit_step["expect"] = "ERROR_CODE_MALFORMED"
    check("validator-power-explicit-zero", _play(devnet_state(), [explicit_step]))

    stacked = devnet_state()
    stacked_problem = None
    for node in ("v1", "v2", "v3"):
        proposal_id = f"rm-{node}"
        payload = validator_power_payload(node, 0, {"height": "30"})
        stacked_problem = _play(
            stacked,
            [
                proposal_step(proposal_id, f"validator-{node}", payload, base=stacked["policy_version"]),
                _approve(proposal_id, "seat-1"),
                _approve(proposal_id, "seat-2"),
                {"op": "CommitBlock", "header_time": stacked["chain_time"] + 10},
                _execute(proposal_id),
            ],
        )
        if stacked_problem:
            break
    if stacked_problem is None:
        proposal_id = "rm-v4"
        payload = validator_power_payload("v4", 0, {"height": "30"})
        stacked_problem = _play(
            stacked,
            [
                proposal_step(proposal_id, "validator-v4", payload, base=stacked["policy_version"]),
                _approve(proposal_id, "seat-1"),
                _approve(proposal_id, "seat-2"),
                {"op": "CommitBlock", "header_time": stacked["chain_time"] + 10},
                _execute(proposal_id) | {"expect": "ERROR_CODE_FORBIDDEN"},
            ],
        )
    if stacked_problem is None and (stacked["pending_power"] and len(stacked["pending_power"]) != 3):
        stacked_problem = "fourth removal was scheduled"
    while stacked_problem is None and stacked["height"] < 30:
        stacked_problem = _play(stacked, [{"op": "CommitBlock", "header_time": stacked["chain_time"] + 1}])
    if stacked_problem is None and (stacked["validators"] != {"v4": 1} or stacked["pending_power"]):
        stacked_problem = "stacked removals did not leave one validator"
    if stacked_problem is None:
        stacked_problem = _play(stacked, [{"op": "CommitBlock", "header_time": stacked["chain_time"] + 1}])
    check("stacked-validator-removal", stacked_problem)

    quorum = devnet_state()
    quorum_id = "rm-quorum"
    quorum_payload = validator_power_payload("v1", 0, {"height": "30"})
    check(
        "validator-removal-breaks-quorum",
        _play(
            quorum,
            [
                proposal_step(quorum_id, "validator-v1", quorum_payload, base=1),
                _approve(quorum_id, "seat-1"),
                _approve(quorum_id, "seat-2"),
                {"op": "CommitBlock", "header_time": CHAIN_TIME + 10},
                {"op": "SetDown", "validators": ["v4"]},
                _execute(quorum_id) | {"expect": "ERROR_CODE_QUORUM"},
            ],
        ),
    )
    if found[-1][1] is None and quorum["pending_power"]:
        found[-1] = (found[-1][0], "quorum-breaking removal was scheduled")

    scheduled_dup = devnet_state()
    first_id = "add-v5"
    first_payload = validator_power_payload("v5", 1, {"height": "20"})
    dup_problem = _play(
        scheduled_dup,
        [
            proposal_step(first_id, "validator-v5", first_payload, base=scheduled_dup["policy_version"]),
            _approve(first_id, "seat-1"),
            _approve(first_id, "seat-2"),
            {"op": "CommitBlock", "header_time": scheduled_dup["chain_time"] + 10},
            _execute(first_id),
        ],
    )
    if dup_problem is None:
        second_id = "add-v6"
        second_payload = validator_power_payload("v6", 1, {"height": "20"}, pubkey_hex=pubkey_for("v5"))
        dup_problem = _play(
            scheduled_dup,
            [
                proposal_step(second_id, "validator-v6", second_payload, base=scheduled_dup["policy_version"]),
                _approve(second_id, "seat-1"),
                _approve(second_id, "seat-2"),
                {"op": "CommitBlock", "header_time": scheduled_dup["chain_time"] + 10},
                _execute(second_id) | {"expect": "ERROR_CODE_DUPLICATE_ID"},
            ],
        )
    if dup_problem is None and len(scheduled_dup["pending_power"]) != 1:
        dup_problem = "duplicate scheduled key was accepted"
    check("duplicate-scheduled-validator-key", dup_problem)
    return found


def main() -> int:
    results = cases()
    problems = [(case_id, detail) for case_id, detail in results if detail]
    print(f"cases {len(results)}")
    for case_id, detail in problems:
        print(f"FAIL {case_id}: {detail}")
    if problems:
        return 1
    print("result PASS")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
