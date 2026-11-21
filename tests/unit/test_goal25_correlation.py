"""Correlation ids on playback responses and media jobs."""

from __future__ import annotations

import http.client
from pathlib import Path

from svn_delivery.catalog import Catalog
from svn_delivery.correlate import correlation_id, redact
from svn_delivery.server import MediaServer
from svn_delivery.tlsutil import client_context
from svn_media.correlate import new_id, valid
from svn_media.correlate import redact as redact_job
from svn_media.queue import Workspace


def test_correlation_id_replaces_an_address() -> None:
    kept = "c" + "ab" * 16
    assert correlation_id(kept) == kept
    fresh = correlation_id("person@example.com")
    assert len(fresh) == 33 and fresh.startswith("c") and "@" not in fresh
    canary = "SVN-SYNTHETIC-CANARY-" + "25"
    cleaned = redact(f"bearer {canary} person@example.com acc_example1")
    assert canary not in cleaned
    assert "person@example.com" not in cleaned
    assert "acc_example1" not in cleaned
    assert "[redacted]" in cleaned


def test_job_correlation_stays_out_of_the_job_file(tmp_path: Path) -> None:
    workspace = Workspace(tmp_path)
    assert workspace.last_correlation_id == ""
    workspace.enqueue(
        job_id="jexample",
        content_id="cexample",
        object_name="ab" * 32,
        output_budget_bytes=10,
        content_version=1,
    )
    assert valid(workspace.last_correlation_id)
    stored = (tmp_path / "jobs" / "queued" / "jexample.json").read_text(encoding="utf-8")
    assert workspace.last_correlation_id not in stored
    assert "correlation" not in stored
    canary = "SVN-SYNTHETIC-CANARY-" + "25"
    assert canary not in redact_job(f"bearer {canary} person@example.com")
    assert valid(new_id())


def test_playback_response_carries_a_correlation_id(tmp_path: Path) -> None:
    catalog = Catalog(tmp_path)
    catalog.note_chain("HELD")
    server = MediaServer(catalog, "origin-test")
    server.start()
    try:
        conn = http.client.HTTPSConnection(
            "127.0.0.1",
            server.port,
            context=client_context(server.cert.cert_pem),
            timeout=5,
        )
        conn.request(
            "OPTIONS",
            "/v1/playback/anything",
            headers={"X-Correlation-ID": "person@example.com"},
        )
        response = conn.getresponse()
        response.read()
        got = response.getheader("X-Correlation-ID")
        assert got is not None
        assert len(got) == 33 and got.startswith("c") and "@" not in got
    finally:
        server.close()
