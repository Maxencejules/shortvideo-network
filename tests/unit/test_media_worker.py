"""Isolated media worker checks. Fixtures are synthetic lavfi sources."""

from __future__ import annotations

import hashlib
import json
import os
import socket
import subprocess
import sys
import threading
import time
from pathlib import Path

import pytest
from svn_media import Failure, Workspace
from svn_media.buildinfo import build_record
from svn_media.encode import Probe, video_arguments
from svn_media.isolate import Bounds, network_namespace_available, run
from svn_media.limits import Limits

from svn_protocol.schemas import validate_manifest

ROOT = Path(__file__).resolve().parents[2]


def _ffmpeg(args: list[str]) -> None:
    proc = subprocess.run(
        ["ffmpeg", "-y", "-hide_banner", "-loglevel", "error", *args],
        capture_output=True,
        check=False,
    )
    if proc.returncode != 0:
        raise AssertionError(proc.stderr.decode("utf-8", "replace"))


def make_clip(
    path: Path, size: str, seconds: str, *, audio: bool = False, rotate: bool = False
) -> None:
    """Synthetic testsrc. No copyrighted or personal media."""
    if rotate:
        base = path.with_name(path.stem + "-base.mp4")
        make_clip(base, size, seconds, audio=False, rotate=False)
        _ffmpeg(["-display_rotation", "90", "-i", str(base), "-c", "copy", str(path)])
        base.unlink()
        return
    if audio:
        _ffmpeg(
            [
                "-f",
                "lavfi",
                "-i",
                f"testsrc=size={size}:rate=10",
                "-f",
                "lavfi",
                "-i",
                "sine=frequency=440:sample_rate=48000",
                "-t",
                seconds,
                "-shortest",
                "-map",
                "0:v:0",
                "-map",
                "1:a:0",
                "-pix_fmt",
                "yuv420p",
                "-c:a",
                "aac",
                str(path),
            ]
        )
        return
    _ffmpeg(
        [
            "-f",
            "lavfi",
            "-i",
            f"testsrc=size={size}:rate=10",
            "-t",
            seconds,
            "-pix_fmt",
            "yuv420p",
            str(path),
        ]
    )


def _name(index: int) -> str:
    return f"{index:064x}"


def _submit(
    ws: Workspace,
    source: Path,
    content_id: str,
    *,
    index: int,
    budget: int = 5_000_000,
    captions: bytes | None = None,
    reviewed: bool = False,
    version: int = 1,
    job_id: str | None = None,
) -> str:
    object_name = _name(index)
    payload = source.read_bytes()
    (ws.root / "quarantine" / object_name).write_bytes(payload)
    job_id = job_id or f"j{index}"
    ws.enqueue(
        job_id=job_id,
        content_id=content_id,
        object_name=object_name,
        output_budget_bytes=budget,
        content_version=version,
        input_sha256=hashlib.sha256(payload).hexdigest(),
        captions=captions,
        captions_reviewed=reviewed,
    )
    return job_id


def _package(ws: Workspace, content_id: str, version: int = 1) -> dict[str, object]:
    path = ws.root / "packages" / content_id / f"v{version}" / "manifest.json"
    document = json.loads(path.read_text(encoding="utf-8"))
    validate_manifest(document)
    return document


def _rendition_ids(document: dict[str, object]) -> set[str]:
    renditions = document["renditions"]
    assert isinstance(renditions, list)
    return {str(item["rendition_id"]) for item in renditions if isinstance(item, dict)}


def _segment_size(
    ws: Workspace, content_id: str, rendition: str, version: int = 1
) -> tuple[int, int]:
    root = ws.root / "packages" / content_id / f"v{version}" / "segments"
    path = next(root.glob(f"{rendition}-0.*"))
    probe = subprocess.run(
        [
            "ffprobe",
            "-v",
            "error",
            "-select_streams",
            "v:0",
            "-show_entries",
            "stream=width,height",
            "-of",
            "csv=p=0",
            str(path),
        ],
        capture_output=True,
        text=True,
        check=True,
    )
    width, height = probe.stdout.strip().split(",")
    return int(width), int(height)


def test_portrait_rotated_lowres_and_audio(tmp_path: Path) -> None:
    ws = Workspace(tmp_path / "work")
    quota = b"input=3 output=3\n"
    (ws.root / "quota.json").write_bytes(quota)
    clips = tmp_path / "clips"
    clips.mkdir()
    portrait = clips / "portrait.mp4"
    rotated = clips / "rotated.mp4"
    low = clips / "low.mp4"
    both = clips / "both.mp4"
    make_clip(portrait, "144x256", "0.3")
    make_clip(rotated, "320x180", "0.3", rotate=True)
    make_clip(low, "96x64", "0.3")
    make_clip(both, "96x64", "0.4", audio=True)
    caption = b"WEBVTT\n\n00:00.000 --> 00:00.200\n; rm -rf /\n"
    _submit(ws, portrait, "cportrait", index=1, captions=caption, reviewed=True)
    _submit(ws, rotated, "crotated", index=2)
    _submit(ws, low, "clow", index=3)
    _submit(ws, both, "cboth", index=4, captions=b"\x00not-text", reviewed=False)
    done = ws.run_once()
    assert done is not None
    assert done["publication"] == "NOT_PUBLISHED"
    assert done["status"] == "READY_FOR_REVIEW"
    assert done["captions_reviewed"] is True
    portrait_doc = _package(ws, "cportrait")
    assert _rendition_ids(portrait_doc) == {"native", "thumb", "captions"}
    assert _segment_size(ws, "cportrait", "native") == (144, 256)
    text = (ws.root / "packages" / "cportrait" / "v1" / "segments" / "captions-0.vtt").read_bytes()
    assert text == caption
    total = 0
    renditions = portrait_doc["renditions"]
    assert isinstance(renditions, list)
    for rendition in renditions:
        assert isinstance(rendition, dict)
        segments = rendition["segments"]
        assert isinstance(segments, list)
        for segment in segments:
            assert isinstance(segment, dict)
            segment_id = str(segment["segment_id"])
            codec = str(rendition["codec"])
            suffix = {"avc1": ".mp4", "jpeg": ".jpg", "webvtt": ".vtt"}[codec]
            folder = ws.root / "packages" / "cportrait" / "v1" / "segments"
            payload = (folder / f"{segment_id}{suffix}").read_bytes()
            assert hashlib.sha256(payload).hexdigest() == segment["sha256"]
            assert str(len(payload)) == segment["byte_count"]
            total += len(payload)
    assert portrait_doc["package_bytes"] == str(total)
    assert done["package_bytes"] == total
    assert (
        done["manifest_sha256"]
        == hashlib.sha256(
            (ws.root / "packages" / "cportrait" / "v1" / "manifest.json").read_bytes()
        ).hexdigest()
    )

    rotated_done = ws.run_once()
    assert rotated_done is not None
    assert _segment_size(ws, "crotated", "native") == (180, 320)
    low_done = ws.run_once()
    assert low_done is not None
    width, height = _segment_size(ws, "clow", "native")
    assert width <= 96 and height <= 64
    assert "h480" not in _rendition_ids(_package(ws, "clow"))
    assert "h720" not in _rendition_ids(_package(ws, "clow"))
    with pytest.raises(Failure) as bad_caption:
        ws.run_once()
    assert bad_caption.value.code == "CAPTION_REJECTED"
    assert "/" not in str(bad_caption.value)
    assert "Traceback" not in str(bad_caption.value)
    assert (ws.root / "quota.json").read_bytes() == quota
    assert not (ws.root / "staging" / "j1").exists()
    assert ws.run_once() is None


def test_malformed_truncated_duration_dimension_and_growth(tmp_path: Path) -> None:
    clips = tmp_path / "clips"
    clips.mkdir()
    good = clips / "good.mp4"
    make_clip(good, "32x32", "0.3")
    bad = clips / "bad.bin"
    bad.write_bytes(b"not-a-video!!")
    ws = Workspace(tmp_path / "malformed")
    _submit(ws, bad, "cm1", index=1)
    with pytest.raises(Failure) as malformed:
        ws.run_once()
    assert malformed.value.code == "MALFORMED"

    cut = clips / "cut.mp4"
    cut.write_bytes(good.read_bytes()[:24])
    ws = Workspace(tmp_path / "truncated")
    _submit(ws, cut, "ct1", index=2)
    with pytest.raises(Failure) as truncated:
        ws.run_once()
    assert truncated.value.code == "TRUNCATED"
    assert not any((ws.root / "staging").iterdir())

    ws = Workspace(tmp_path / "duration", limits=Limits(max_duration_seconds=1))
    long = clips / "long.mp4"
    make_clip(long, "32x32", "2")
    _submit(ws, long, "cd1", index=3)
    with pytest.raises(Failure) as over:
        ws.run_once()
    assert over.value.code == "OVER_DURATION"

    ws = Workspace(tmp_path / "dimension", limits=Limits(max_dimension=16))
    _submit(ws, good, "cz1", index=4)
    with pytest.raises(Failure) as big:
        ws.run_once()
    assert big.value.code == "OVER_DIMENSION"

    ws = Workspace(tmp_path / "growth")
    _submit(ws, good, "cg1", index=5, budget=100)
    with pytest.raises(Failure) as growth:
        ws.run_once()
    assert growth.value.code == "OUTPUT_GROWTH"
    assert not (ws.root / "packages" / "cg1").exists()


def test_retry_is_one_package_and_reencode_is_a_new_version(tmp_path: Path) -> None:
    ws = Workspace(tmp_path / "work")
    clip = tmp_path / "clip.mp4"
    make_clip(clip, "64x64", "0.2")
    _submit(ws, clip, "cretry", index=6)
    first = ws.run_once()
    assert first is not None
    assert first["content_version"] == 1
    assert ws.run_once() is None
    versions = list((ws.root / "packages" / "cretry").iterdir())
    assert [path.name for path in versions] == ["v1"]
    with pytest.raises(Failure) as duplicate:
        _submit(ws, clip, "cretry", index=6)
    assert duplicate.value.code == "REFUSED"
    new_id = ws.reencode("cretry")
    second = ws.run_once()
    assert second is not None
    assert second["job_id"] == new_id
    assert second["content_version"] == 2
    assert second["publication"] == "NOT_PUBLISHED"
    assert (ws.root / "packages" / "cretry" / "v1" / "manifest.json").is_file()
    assert (ws.root / "packages" / "cretry" / "v2" / "manifest.json").is_file()
    assert _package(ws, "cretry", 1)["content_version"] == 1
    assert _package(ws, "cretry", 2)["content_version"] == 2


def test_cancel_removes_staging_and_does_not_publish(tmp_path: Path) -> None:
    ws = Workspace(tmp_path / "work")
    clip = tmp_path / "clip.mp4"
    make_clip(clip, "32x32", "0.2")
    _submit(ws, clip, "ccancel", index=7)
    ws.cancel("j7")
    failed = json.loads((ws.root / "jobs" / "failed" / "j7.json").read_text(encoding="utf-8"))
    assert failed == {
        "code": "CANCELLED",
        "job_id": "j7",
        "message": "Processing was cancelled.",
    }
    assert ws.run_once() is None
    assert not (ws.root / "packages" / "ccancel").exists()
    assert not (ws.root / "staging" / "j7").exists()


def test_kill_restart_keeps_one_package_and_quota(tmp_path: Path) -> None:
    ws_root = tmp_path / "work"
    ws = Workspace(ws_root)
    quota = b"reserved-input=9 reserved-output=9\n"
    (ws.root / "quota.json").write_bytes(quota)
    clip = tmp_path / "clip.mp4"
    make_clip(clip, "64x48", "0.3")
    _submit(ws, clip, "ckill", index=8)
    env = _child_env("after-segment")
    proc = subprocess.Popen(
        [sys.executable, "-m", "svn_media", "run", "--root", str(ws_root)],
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline and not (ws_root / "fault-ready").exists():
        if proc.poll() is not None:
            raise AssertionError(proc.stdout.read() if proc.stdout else b"")
        time.sleep(0.05)
    assert (ws_root / "fault-ready").is_file()
    proc.kill()
    proc.wait(timeout=5)
    assert not (ws_root / "packages" / "ckill").exists()
    assert (ws_root / "jobs" / "running" / "j8.json").is_file()
    assert (ws_root / "quota.json").read_bytes() == quota
    again = subprocess.run(
        [sys.executable, "-m", "svn_media", "run", "--root", str(ws_root)],
        env=_child_env(None),
        capture_output=True,
        text=True,
        check=False,
    )
    assert again.returncode == 0, again.stdout + again.stderr
    body = json.loads(again.stdout.strip().splitlines()[-1])
    assert body["publication"] == "NOT_PUBLISHED"
    assert body["status"] == "READY_FOR_REVIEW"
    assert body["content_version"] == 1
    names = sorted(path.name for path in (ws_root / "packages" / "ckill").iterdir())
    assert names == ["v1"]
    assert not (ws_root / "staging" / "j8").exists()
    assert not (ws_root / "jobs" / "running" / "j8.json").exists()
    assert (ws_root / "quota.json").read_bytes() == quota
    published = "PUBLISHED"
    for path in ws_root.rglob("*"):
        if not path.is_file():
            continue
        text = path.read_text(encoding="utf-8", errors="ignore")
        assert f'"status":"{published}"' not in text.replace(" ", "")
    third = subprocess.run(
        [sys.executable, "-m", "svn_media", "run", "--root", str(ws_root)],
        env=_child_env(None),
        capture_output=True,
        text=True,
        check=False,
    )
    assert third.returncode == 0
    assert third.stdout.strip() == ""


def test_symlink_and_validator_key_are_refused(tmp_path: Path) -> None:
    root = tmp_path / "keyed"
    root.mkdir()
    (root / "node_key.json").write_text("{}\n", encoding="utf-8")
    with pytest.raises(Failure) as refused:
        Workspace(root)
    assert refused.value.code == "REFUSED"
    ws = Workspace(tmp_path / "link")
    clip = tmp_path / "clip.mp4"
    make_clip(clip, "32x32", "0.2")
    object_name = _name(9)
    (ws.root / "quarantine" / object_name).symlink_to(clip)
    ws.enqueue(
        job_id="j9",
        content_id="clink",
        object_name=object_name,
        output_budget_bytes=5_000_000,
        content_version=1,
    )
    with pytest.raises(Failure) as linked:
        ws.run_once()
    assert linked.value.code == "REFUSED"


def test_captions_are_not_command_arguments() -> None:
    probe = Probe(0.2, 32, 32, False, 0, False)
    argv = video_arguments(
        "/usr/bin/ffmpeg",
        Path("/tmp/in.mp4"),
        Path("/tmp/out.mp4"),
        probe=probe,
        width=32,
        height=32,
    )
    assert isinstance(argv, list)
    assert "file,crypto,pipe" in argv
    assert "-protocol_whitelist" in argv
    joined = "\n".join(argv)
    assert "rm -rf" not in joined
    assert "WEBVTT" not in joined
    assert not any(arg.startswith("-vf") and "WEBVTT" in arg for arg in argv)


def test_network_namespace_and_resource_limits(tmp_path: Path) -> None:
    server = socket.socket()
    server.bind(("127.0.0.1", 0))
    server.listen(2)
    server.settimeout(2)
    port = server.getsockname()[1]

    def _accept() -> None:
        for _ in range(2):
            try:
                conn, _addr = server.accept()
            except OSError:
                return
            conn.close()

    threading.Thread(target=_accept, daemon=True).start()
    outer = socket.create_connection(("127.0.0.1", port), 2)
    outer.close()
    netns = network_namespace_available()
    bounds = Bounds(
        wall_seconds=10, cpu_seconds=5, memory_bytes=256 * 1024 * 1024, file_bytes=1_000_000
    )
    script = f"import socket; socket.create_connection(('127.0.0.1', {port}), 2)"
    connect = run(
        [sys.executable, "-c", script],
        cwd=tmp_path,
        bounds=bounds,
    )
    server.close()
    if netns:
        assert connect.returncode != 0
    else:
        assert connect.returncode == 0
    identity = run(
        [
            sys.executable,
            "-c",
            "print(open('/proc/self/uid_map').read());"
            "import resource;"
            "print(resource.getrlimit(resource.RLIMIT_AS)[0]);"
            "print(resource.getrlimit(resource.RLIMIT_CPU)[0]);"
            "print(resource.getrlimit(resource.RLIMIT_FSIZE)[0])",
        ],
        cwd=tmp_path,
        bounds=bounds,
    )
    assert identity.returncode == 0
    lines = [line for line in identity.stdout.decode().splitlines() if line.strip()]
    uid_map = lines[0].split()
    if netns:
        assert uid_map[0] == "0"
        assert uid_map[1] != "0"
    assert lines[1] == str(bounds.memory_bytes)
    assert lines[2] == str(bounds.cpu_seconds)
    assert lines[3] == str(bounds.file_bytes)
    blocked = run(
        [sys.executable, "-c", "open('/usr/bin/svn-media-probe','w').close()"],
        cwd=tmp_path,
        bounds=bounds,
    )
    assert blocked.returncode != 0
    http = run(
        [
            "ffmpeg",
            "-hide_banner",
            "-nostdin",
            "-protocol_whitelist",
            "file,crypto,pipe",
            "-i",
            "http://127.0.0.1/x",
            "-f",
            "null",
            "-",
        ],
        cwd=tmp_path,
        bounds=bounds,
    )
    assert http.returncode != 0
    with pytest.raises(Failure) as limited:
        run(
            [sys.executable, "-c", "import time; time.sleep(5)"],
            cwd=tmp_path,
            bounds=Bounds(
                wall_seconds=1, cpu_seconds=5, memory_bytes=256 * 1024 * 1024, file_bytes=1_000_000
            ),
        )
    assert limited.value.code == "OVER_BUDGET"
    disk_bounds = Bounds(
        wall_seconds=10, cpu_seconds=5, memory_bytes=256 * 1024 * 1024, file_bytes=4096
    )
    small = run(
        [sys.executable, "-c", "open('small','wb').write(b'x'*100)"],
        cwd=tmp_path,
        bounds=disk_bounds,
    )
    assert small.returncode == 0
    big = run(
        [sys.executable, "-c", "open('big','wb').write(b'x'*100000)"],
        cwd=tmp_path,
        bounds=disk_bounds,
    )
    assert big.returncode != 0


def test_ffmpeg_build_record_names_the_gpl_package() -> None:
    text = build_record()
    assert "6.1.1-3ubuntu5" in text
    assert "--enable-gpl" in text
    assert "DEC-006 is open" in text
    assert "libx264" in text


def test_worker_is_outside_the_api_process() -> None:
    api = ROOT / "services" / "api"
    text = "\n".join(path.read_text(encoding="utf-8") for path in api.rglob("*.go"))
    assert "svn_media" not in text
    assert "os/exec" in (api / "internal" / "service" / "probe.go").read_text(encoding="utf-8")


def test_python_job_installs_ffmpeg_before_tests() -> None:
    import yaml

    workflow = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
    document = yaml.safe_load(workflow)
    steps = document["jobs"]["python"]["steps"]
    runs = [step.get("run") or "" for step in steps]
    install_at = next(index for index, run in enumerate(runs) if "ffmpeg=7:6.1.1-3ubuntu5" in run)
    test_at = next(index for index, run in enumerate(runs) if "run_tests.py --only python" in run)
    assert install_at < test_at


def test_limits_stay_proposed() -> None:
    with pytest.raises(Failure):
        Limits(label="APPROVED")
    with pytest.raises(Failure):
        Limits(max_duration_seconds=61)
    with pytest.raises(Failure):
        Limits(max_input_bytes=100_000_001)


@pytest.mark.skipif(
    os.environ.get("SVN_MEDIA_TIMING") != "1", reason="set SVN_MEDIA_TIMING=1 to measure"
)
def test_sixty_second_input_timing(tmp_path: Path) -> None:
    ws = Workspace(tmp_path / "work")
    clip = tmp_path / "minute.mp4"
    make_clip(clip, "1280x720", "60", audio=True)
    _submit(ws, clip, "cminute", index=10, budget=80_000_000)
    started = time.monotonic()
    done = ws.run_once()
    elapsed = time.monotonic() - started
    print(f"elapsed_seconds={elapsed:.3f}")
    assert done is not None
    ids = _rendition_ids(_package(ws, "cminute"))
    assert "h480" in ids and "h720" in ids and "native" not in ids
    assert elapsed <= 120


def _child_env(fault: str | None) -> dict[str, str]:
    env = {
        "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
        "PYTHONPATH": f"{ROOT / 'workers' / 'media'}{os.pathsep}{ROOT / 'packages' / 'protocol'}",
        "LC_ALL": "C.UTF-8",
        "TZ": "UTC",
        "HOME": os.environ.get("HOME", "/tmp"),
    }
    if fault:
        env["SVN_MEDIA_TIMING"] = ""
        env["SVN_MEDIA_FAULT"] = fault
    return env
