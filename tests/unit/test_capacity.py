"""Gate arithmetic for the capacity runner. This file does not encode or browse."""

from __future__ import annotations

from pathlib import Path

import pytest
from scripts.capacity import (
    GOODPUT_HI_MBPS,
    GOODPUT_LO_MBPS,
    PING_HI_MS,
    PING_LO_MS,
    PLAY_SAMPLES,
    PROCESSING_LIMIT_S,
    REBUFFER_LIMIT,
    SHAPE_BODY_BYTES,
    SHAPE_BURST,
    SHAPE_DELAY,
    SHAPE_LATENCY,
    SHAPE_RATE,
    STARTUP_LIMIT_MS,
    WORKER_CONCURRENCY,
    arithmetic,
    design_point_gate,
    illustrative_transactions,
    nearest_rank_p95,
    playback_gate,
    processing_gate,
    rebuffer_ratio,
    shape_confirmed,
    stored_gb,
    viewer_payload_gb,
)
from svn_delivery.browser import capacity_page_html, page_html
from svn_delivery.errors import DeliveryError
from svn_delivery.server import _clean_report

ROOT = Path(__file__).resolve().parents[2]


def test_blueprint_arithmetic_is_the_page_23_table() -> None:
    assert viewer_payload_gb(100, 20, 2, 30) == 900
    assert stored_gb(20, 25, 3, 30) == 45
    assert illustrative_transactions(200, 4) == 800
    sheet = arithmetic()
    assert sheet["label"] == "blueprint arithmetic, not a measurement"
    assert sheet["peer_share"] is None
    assert sheet["illustrative_tx_per_second"] == "800/86400"


def test_nearest_rank_and_rebuffer_denominator() -> None:
    assert nearest_rank_p95(list(range(1, 9))) == 8
    assert nearest_rank_p95(list(range(1, 21))) == 19
    assert rebuffer_ratio(600, 60_000) == pytest.approx(0.01)
    assert rebuffer_ratio(0, 60_000) == 0
    with pytest.raises(ValueError):
        rebuffer_ratio(0, 0)


def test_gates_do_not_pass_a_missing_run() -> None:
    assert processing_gate(None) == "BLOCKED"
    assert processing_gate(PROCESSING_LIMIT_S) == "PASS"
    assert processing_gate(PROCESSING_LIMIT_S + 0.001) == "FAIL"
    assert design_point_gate(0, 1) == "BLOCKED"
    assert design_point_gate(100, 10) == "BLOCKED"
    assert (
        playback_gate(
            shape_ok=False,
            sample_size=PLAY_SAMPLES,
            failures=0,
            p95_render_ms=1,
            ratio=0,
        )
        == "BLOCKED"
    )
    assert (
        playback_gate(
            shape_ok=True,
            sample_size=PLAY_SAMPLES,
            failures=1,
            p95_render_ms=1,
            ratio=0,
        )
        == "FAIL"
    )
    assert (
        playback_gate(
            shape_ok=True,
            sample_size=PLAY_SAMPLES,
            failures=0,
            p95_render_ms=STARTUP_LIMIT_MS,
            ratio=REBUFFER_LIMIT,
        )
        == "PASS"
    )
    assert (
        playback_gate(
            shape_ok=True,
            sample_size=PLAY_SAMPLES,
            failures=0,
            p95_render_ms=STARTUP_LIMIT_MS + 1,
            ratio=0,
        )
        == "FAIL"
    )


def test_shape_band_is_closed() -> None:
    assert shape_confirmed(PING_LO_MS, GOODPUT_LO_MBPS, 0) is True
    assert shape_confirmed(PING_HI_MS, GOODPUT_HI_MBPS, 0) is True
    assert shape_confirmed(PING_LO_MS - 0.1, 20, 0) is False
    assert shape_confirmed(50, GOODPUT_HI_MBPS + 0.1, 0) is False
    assert shape_confirmed(50, 20, 0.1) is False


def test_load_plan_states_the_procedure() -> None:
    text = (ROOT / "docs" / "operations" / "load-plan.md").read_text(encoding="utf-8")
    for phrase in (
        SHAPE_DELAY,
        SHAPE_RATE,
        SHAPE_BURST,
        SHAPE_LATENCY,
        "5000000",
        "stall_ms / media_ms",
        "(95 * n + 99) // 100",
        "Worker concurrency",
        "8, sequential",
        "not an operating budget",
        "not a universal hardware minimum",
        "Peer share is not measured",
        "900 GB",
        "45 GB",
        "800/86400",
        "DR-28-01",
        "DR-28-02",
    ):
        assert phrase in text
    assert WORKER_CONCURRENCY == 1
    assert SHAPE_BODY_BYTES == 5_000_000


def test_capacity_page_uses_the_product_variant_and_a_frame() -> None:
    plain = page_html("cminute")
    played = capacity_page_html("cminute")
    assert "renderMs" not in plain
    assert "lines[lines.length - 1]" in plain
    assert "variants[0]" in played
    assert "renderMs" in played
    assert "stallMs" in played
    with pytest.raises(DeliveryError):
        capacity_page_html("../x")


def test_report_keeps_optional_times_and_rejects_a_bool() -> None:
    base = {
        "rendered": True,
        "caption": False,
        "error": "",
        "sha256": "",
        "videoWidth": 1,
        "videoHeight": 1,
        "readyState": 2,
        "nonzero": 1,
    }
    plain = _clean_report(base)
    assert "renderMs" not in plain
    rich = dict(base)
    rich.update(
        {
            "renderMs": 3000,
            "fetchMs": 1000,
            "stallMs": 0,
            "mediaMs": 60000,
            "segment": "segments/h720-0.mp4",
        }
    )
    cleaned = _clean_report(rich)
    assert cleaned["renderMs"] == 3000
    assert cleaned["segment"] == "segments/h720-0.mp4"
    rejected = dict(rich)
    rejected["renderMs"] = True
    with pytest.raises(DeliveryError):
        _clean_report(rejected)
    rejected["renderMs"] = 600_001
    with pytest.raises(DeliveryError):
        _clean_report(rejected)
