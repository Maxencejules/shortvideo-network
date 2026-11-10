"""A playback grant written by a second process is visible. A host binding is not."""

from pathlib import Path

from svn_delivery.catalog import Catalog


def test_a_second_catalog_does_not_restore_a_host(tmp_path: Path) -> None:
    catalog = Catalog(tmp_path)
    catalog.note_chain("HELD")
    catalog.bind_host("origin-test", "https://127.0.0.1:9", role="origin")
    again = Catalog(tmp_path)
    assert "origin-test" not in again._hosts


def test_a_running_catalog_reloads_a_grant(tmp_path: Path) -> None:
    serving = Catalog(tmp_path)
    serving.note_chain("HELD")
    serving.bind_host("origin-test", "https://127.0.0.1:9", role="origin")
    # A second process writes the grant. This catalog has no package, so the
    # reload is what makes the grant visible. authorize needs a package, so
    # write the grant file the same way a second process would after authorize.
    playback = tmp_path / "playback"
    playback.mkdir(exist_ok=True)
    (playback / "c1.json").write_text(
        '{"content_id":"c1","content_version":1,"manifest_sha256":"'
        + "ab" * 32
        + '","state":"AUTHORIZED","authorized_until":9999999999,'
        + '"host_ids":["origin-test"],"hosting_promise":false}',
        encoding="utf-8",
    )
    assert serving.grant_live("c1") is True
