#!/usr/bin/env python3
"""Bounded capacity measurements for this machine.

The design point of 100 viewers and 10 uploads is not started. A missing
network shape is BLOCKED. A segment download without a decoded frame is not
playback success.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import re
import shutil
import subprocess
import sys
import threading
import time
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import cast

ROOT = Path(__file__).resolve().parents[1]

SHAPE_DELAY = "25ms"
SHAPE_RATE = "20mbit"
SHAPE_BURST = "256kbit"
SHAPE_LATENCY = "400ms"
PING_LO_MS = 40.0
PING_HI_MS = 80.0
GOODPUT_LO_MBPS = 10.0
GOODPUT_HI_MBPS = 30.0
SHAPE_BODY_BYTES = 5_000_000
PLAY_SAMPLES = 8
PROCESSING_LIMIT_S = 120.0
STARTUP_LIMIT_MS = 3000
REBUFFER_LIMIT = 0.01
WORKER_CONCURRENCY = 1
NETNS = "svncap28"


def viewer_payload_gb(dau: int, minutes: int, bitrate_mbps: int, days: int) -> int:
    """Blueprint page 23 arithmetic. Decimal gigabytes."""
    return dau * minutes * 60 * bitrate_mbps * days // 8000


def stored_gb(uploads: int, package_mb: int, replicas: int, days: int) -> int:
    """Blueprint page 23 arithmetic. Decimal gigabytes."""
    return uploads * package_mb * replicas * days // 1000


def illustrative_transactions(uploads_per_day: int, messages_per_upload: int) -> int:
    return uploads_per_day * messages_per_upload


def nearest_rank_p95(values: list[int]) -> int:
    """Nearest-rank p95. Rank is `(95 * n + 99) // 100`, counting from 1."""
    if not values:
        raise ValueError("empty sample")
    ordered = sorted(values)
    rank = (95 * len(ordered) + 99) // 100
    return ordered[rank - 1]


def rebuffer_ratio(stall_ms: int, media_ms: int) -> float:
    """`stall_ms / media_ms`. The denominator is presented media time."""
    if media_ms <= 0:
        raise ValueError("media denominator")
    if stall_ms < 0:
        raise ValueError("stall")
    return stall_ms / media_ms


def processing_gate(elapsed_s: float | None) -> str:
    if elapsed_s is None or math.isnan(elapsed_s):
        return "BLOCKED"
    if elapsed_s <= PROCESSING_LIMIT_S:
        return "PASS"
    return "FAIL"


def shape_confirmed(rtt_ms: float, goodput_mbps: float, loss_pct: float) -> bool:
    rtt_ok = PING_LO_MS <= rtt_ms <= PING_HI_MS
    rate_ok = GOODPUT_LO_MBPS <= goodput_mbps <= GOODPUT_HI_MBPS
    return rtt_ok and rate_ok and loss_pct == 0.0


def design_point_gate(viewers_started: int, uploads_started: int) -> str:
    """This runner does not award PASS for the design point."""
    if viewers_started < 0 or uploads_started < 0:
        raise ValueError("counts")
    return "BLOCKED"


def playback_gate(
    *,
    shape_ok: bool,
    sample_size: int,
    failures: int,
    p95_render_ms: int | None,
    ratio: float | None,
) -> str:
    if not shape_ok or sample_size != PLAY_SAMPLES:
        return "BLOCKED"
    if failures or p95_render_ms is None or ratio is None:
        return "FAIL"
    if p95_render_ms <= STARTUP_LIMIT_MS and ratio <= REBUFFER_LIMIT:
        return "PASS"
    return "FAIL"


def arithmetic() -> dict[str, object]:
    viewers = viewer_payload_gb(100, 20, 2, 30)
    stored = stored_gb(20, 25, 3, 30)
    txs = illustrative_transactions(200, 4)
    return {
        "label": "blueprint arithmetic, not a measurement",
        "small_beta_viewer_gb": viewers,
        "small_beta_storage_gb": stored,
        "illustrative_tx_per_day": txs,
        "illustrative_tx_per_second": f"{txs}/86400",
        "peer_share": None,
    }


def _run(
    argv: list[str],
    *,
    cwd: Path | None = None,
    env: dict[str, str] | None = None,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        argv,
        cwd=cwd or ROOT,
        env=env,
        text=True,
        capture_output=True,
        check=False,
    )


def _read_meminfo(prefix: str) -> int:
    for line in Path("/proc/meminfo").read_text(encoding="utf-8").splitlines():
        if line.startswith(prefix):
            return int(line.split()[1])
    return 0


def _cpu() -> tuple[int, int]:
    parts = Path("/proc/stat").read_text(encoding="utf-8").splitlines()[0].split()
    nums = [int(item) for item in parts[1:]]
    return sum(nums), nums[3]


def _tree_rss_kib(root: int) -> int:
    parent_of: dict[int, int] = {}
    rss: dict[int, int] = {}
    for name in os.listdir("/proc"):
        if not name.isdigit():
            continue
        pid = int(name)
        try:
            text = Path(f"/proc/{pid}/status").read_text(encoding="utf-8")
        except OSError:
            continue
        parent = 0
        size = 0
        for line in text.splitlines():
            if line.startswith("PPid:"):
                parent = int(line.split()[1])
            elif line.startswith("VmRSS:"):
                size = int(line.split()[1])
        parent_of[pid] = parent
        rss[pid] = size
    kids: dict[int, list[int]] = {}
    for pid, parent in parent_of.items():
        kids.setdefault(parent, []).append(pid)
    total = 0
    stack = [root]
    seen: set[int] = set()
    while stack:
        pid = stack.pop()
        if pid in seen:
            continue
        seen.add(pid)
        total += rss.get(pid, 0)
        stack.extend(kids.get(pid, ()))
    return total


class _Sampler:
    def __init__(self) -> None:
        self._stop = threading.Event()
        self._peak = 0
        self._min_avail = _read_meminfo("MemAvailable:")
        self._cpu0 = _cpu()
        self._thread = threading.Thread(target=self._loop, daemon=True)

    def start(self) -> None:
        self._thread.start()

    def _loop(self) -> None:
        root = os.getpid()
        while not self._stop.is_set():
            self._peak = max(self._peak, _tree_rss_kib(root))
            avail = _read_meminfo("MemAvailable:")
            if avail and avail < self._min_avail:
                self._min_avail = avail
            self._stop.wait(0.25)

    def finish(self) -> dict[str, object]:
        self._stop.set()
        self._thread.join(timeout=2)
        total1, idle1 = _cpu()
        total0, idle0 = self._cpu0
        delta = total1 - total0
        busy = (delta - (idle1 - idle0)) / delta if delta else 0.0
        return {
            "cpu_busy_ratio": round(busy, 4),
            "cpu_definition": "(total-idle)/total from /proc/stat; iowait counts as busy",
            "peak_tree_rss_kib": self._peak,
            "min_mem_available_kib": self._min_avail,
        }


def hardware() -> dict[str, object]:
    model = ""
    for line in Path("/proc/cpuinfo").read_text(encoding="utf-8").splitlines():
        if line.startswith("model name"):
            model = line.split(":", 1)[1].strip()
            break
    os_name = ""
    for line in Path("/etc/os-release").read_text(encoding="utf-8").splitlines():
        if line.startswith("PRETTY_NAME="):
            os_name = line.split("=", 1)[1].strip().strip('"')
    uname = _run(["uname", "-srm"])
    chrome = _run(["google-chrome", "--version"])
    ffmpeg = _run(["ffmpeg", "-version"])
    head = _run(["git", "rev-parse", "HEAD"])
    tree = _run(["git", "rev-parse", "HEAD^{tree}"])
    return {
        "head": head.stdout.strip(),
        "tree": tree.stdout.strip(),
        "nproc": os.cpu_count(),
        "model": model,
        "mem_total_kib": _read_meminfo("MemTotal:"),
        "os": os_name,
        "uname": uname.stdout.strip(),
        "ffmpeg": ffmpeg.stdout.splitlines()[0] if ffmpeg.stdout else "",
        "chrome": chrome.stdout.strip(),
        "tc": shutil.which("tc") or "absent",
        "ping": shutil.which("ping") or "absent",
        "psql": shutil.which("psql") or "absent",
        "docker": shutil.which("docker") or "absent",
        "worker_concurrency": WORKER_CONCURRENCY,
        "not_a_universal_minimum": True,
        "not_an_operating_budget": True,
    }


def _ffmpeg(args: list[str]) -> None:
    proc = subprocess.run(
        ["ffmpeg", "-y", "-hide_banner", "-loglevel", "error", *args],
        capture_output=True,
        check=False,
    )
    if proc.returncode != 0:
        raise RuntimeError(proc.stderr.decode("utf-8", "replace")[-500:])


def processing(root: Path) -> dict[str, object]:
    from svn_media import Workspace

    clip = root / "minute.mp4"
    gen_started = time.monotonic()
    _ffmpeg(
        [
            "-f",
            "lavfi",
            "-i",
            "testsrc=size=1280x720:rate=10",
            "-f",
            "lavfi",
            "-i",
            "sine=frequency=440:sample_rate=48000",
            "-t",
            "60",
            "-shortest",
            "-map",
            "0:v:0",
            "-map",
            "1:a:0",
            "-pix_fmt",
            "yuv420p",
            "-c:a",
            "aac",
            str(clip),
        ]
    )
    generate_s = time.monotonic() - gen_started
    payload = clip.read_bytes()
    ws = Workspace(root / "work")
    object_name = f"{10:064x}"
    (ws.root / "quarantine" / object_name).write_bytes(payload)
    ws.enqueue(
        job_id="j10",
        content_id="cminute",
        object_name=object_name,
        output_budget_bytes=80_000_000,
        content_version=1,
        input_sha256=hashlib.sha256(payload).hexdigest(),
    )
    sampler = _Sampler()
    sampler.start()
    started = time.monotonic()
    record = ws.run_once()
    elapsed = time.monotonic() - started
    resources = sampler.finish()
    if record is None:
        return {"gate": "FAIL", "reason": "no job"}
    manifest_path = ws.root / "packages" / "cminute" / "v1" / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    renditions = manifest.get("renditions")
    names: list[str] = []
    if isinstance(renditions, list):
        for item in renditions:
            if isinstance(item, dict) and isinstance(item.get("rendition_id"), str):
                names.append(item["rendition_id"])
    return {
        "gate": processing_gate(elapsed),
        "elapsed_seconds": round(elapsed, 3),
        "generate_seconds": round(generate_s, 3),
        "status": record.get("status"),
        "publication": record.get("publication"),
        "package_bytes": record.get("package_bytes"),
        "renditions": names,
        "concurrency": WORKER_CONCURRENCY,
        "codec": "libx264 veryfast",
        "input": "synthetic testsrc 1280x720 10fps sine 440Hz 60s",
        "moderation": {
            "gate": "BLOCKED",
            "reason": "DEC-003 is open; this object is not moderated",
        },
        "replication": {
            "gate": "BLOCKED",
            "reason": "the 60-second package was not handed to the scheduler",
        },
        "publication_phase": {
            "gate": "BLOCKED",
            "reason": "the worker record is NOT_PUBLISHED",
        },
        "resources": resources,
        "limit_seconds": PROCESSING_LIMIT_S,
    }


def _kv_line(text: str, prefix: str) -> dict[str, str]:
    for line in text.splitlines():
        if line.startswith(prefix):
            return dict(part.split("=", 1) for part in line.split() if "=" in part)
    return {}


def chain() -> dict[str, object]:
    proc = _run(
        [
            "go",
            "test",
            "-tags",
            "svn_capacity",
            "-count=1",
            "-timeout",
            "4m",
            "-run",
            "TestCapacityFixture",
            "./chain/app",
        ]
    )
    parsed = _kv_line(proc.stdout, "CAPACITY_CHAIN ")
    return {
        "exit": proc.returncode,
        "fields": parsed,
        "gate": "RECORDED" if proc.returncode == 0 and parsed else "FAIL",
        "tail": (proc.stdout + proc.stderr)[-2000:],
        "scope": "in-process ABCI, two nodes, one machine; not CometBFT throughput",
        "index_lag": {
            "gate": "BLOCKED",
            "reason": "postgresql is absent; the indexer was not started",
        },
        "comet_commit_interval": {
            "gate": "BLOCKED",
            "reason": "this fixture does not run CometBFT",
        },
    }


def scheduler() -> dict[str, object]:
    proc = _run(
        [
            "go",
            "test",
            "-tags",
            "svn_capacity",
            "-count=1",
            "-timeout",
            "6m",
            "-run",
            "TestCapacityFixture",
            "./services/scheduler/cmd/svn-scheduler",
        ]
    )
    text = proc.stdout
    return {
        "exit": proc.returncode,
        "publish": _kv_line(text, "CAPACITY_PUBLISH "),
        "repair": _kv_line(text, "CAPACITY_REPAIR "),
        "slow": _kv_line(text, "CAPACITY_SLOW "),
        "gate": "RECORDED" if proc.returncode == 0 else "FAIL",
        "tail": (text + proc.stderr)[-2000:],
        "multi_host": {"gate": "BLOCKED", "reason": "one machine; provider labels are simulated"},
    }


def _ping(text: str) -> tuple[float, float] | None:
    loss = re.search(r"(\d+(?:\.\d+)?)% packet loss", text)
    rtt = re.search(r"rtt min/avg/max/mdev = [\d.]+/([\d.]+)/", text)
    if loss is None or rtt is None:
        return None
    return float(rtt.group(1)), float(loss.group(1))


def _goodput() -> float:
    body = b"x" * SHAPE_BODY_BYTES

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:  # noqa: N802
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, fmt: str, *args: object) -> None:
            return

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        bound = server.server_address
        port = bound[1]
        if not isinstance(port, int):
            raise RuntimeError("port")
        started = time.monotonic()
        with urllib.request.urlopen(f"http://127.0.0.1:{port}/", timeout=60) as response:
            got = response.read()
        elapsed = time.monotonic() - started
    finally:
        server.shutdown()
        thread.join(timeout=2)
    if len(got) != len(body) or elapsed <= 0:
        raise RuntimeError("short read")
    return (len(got) * 8) / elapsed / 1_000_000


def _duration_ms(path: Path) -> int:
    proc = subprocess.run(
        [
            "ffprobe",
            "-v",
            "error",
            "-show_entries",
            "format=duration",
            "-of",
            "csv=p=0",
            str(path),
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    return int(round(float(proc.stdout.strip()) * 1000))


def _plays(root: Path, content_id: str) -> dict[str, object]:
    os.environ["SVN_CAPACITY_PLAYTHROUGH"] = "1"
    from svn_delivery.browser import run_until
    from svn_delivery.catalog import Catalog
    from svn_delivery.server import MediaServer

    catalog = Catalog(root / "work")
    catalog.record_hits = True
    catalog.record_timing = True
    catalog.note_chain("HELD")
    server = MediaServer(catalog)
    server.start()
    samples: list[dict[str, object]] = []
    try:
        catalog.authorize(content_id, seconds=3600)
        package = root / "work" / "packages" / content_id / "v1"
        videos = list((package / "segments").glob("h720-0.mp4"))
        duration_ms = _duration_ms(videos[0]) if videos else 0
        for index in range(PLAY_SAMPLES):
            catalog.cache.drop(content_id)
            catalog.clear_timings()
            catalog.clear_report(content_id)
            catalog.clear_hits()
            url = f"https://127.0.0.1:{server.port}/v1/player/{content_id}"
            launch = run_until(url, lambda: catalog.report(content_id), timeout=110)
            report = catalog.report(content_id) or {}
            samples.append(
                {
                    "index": index,
                    "launch": launch,
                    "report": report,
                    "timings": list(catalog.timings()),
                    "duration_ms": duration_ms,
                }
            )
    finally:
        server.close()
    return {"samples": samples, "content_id": content_id}


def _sample_ok(sample: dict[str, object], root: Path, content_id: str) -> tuple[bool, str]:
    report = sample.get("report")
    if not isinstance(report, dict) or report.get("rendered") is not True:
        return False, "not rendered"
    segment = report.get("segment")
    if not isinstance(segment, str):
        return False, "segment"
    path = root / "work" / "packages" / content_id / "v1" / segment
    if not path.is_file():
        return False, "file"
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    if report.get("sha256") != digest:
        return False, "sha"
    duration = sample.get("duration_ms")
    media = report.get("mediaMs")
    if not isinstance(duration, int) or not isinstance(media, int):
        return False, "media"
    if media < int(duration * 0.9):
        return False, "short watch"
    timings = sample.get("timings")
    if not isinstance(timings, list):
        return False, "timings"
    size = path.stat().st_size
    matched = [
        item
        for item in timings
        if isinstance(item, dict)
        and str(item.get("path", "")).endswith(segment)
        and item.get("status") == 200
        and item.get("bytes") == size
    ]
    if not matched:
        return False, "server"
    return True, "ok"


def _summarize(raw: dict[str, object], root: Path) -> dict[str, object]:
    content_id = str(raw.get("content_id", ""))
    samples = raw.get("samples")
    if not isinstance(samples, list):
        return {"gate": "FAIL", "reason": "no samples"}
    failures = 0
    starts: list[int] = []
    stall = 0
    media = 0
    rows: list[dict[str, object]] = []
    for sample in samples:
        if not isinstance(sample, dict):
            failures += 1
            continue
        ok, reason = _sample_ok(sample, root, content_id)
        report = sample.get("report")
        render_ms = report.get("renderMs") if isinstance(report, dict) else None
        fetch_ms = report.get("fetchMs") if isinstance(report, dict) else None
        stall_ms = report.get("stallMs") if isinstance(report, dict) else None
        media_ms = report.get("mediaMs") if isinstance(report, dict) else None
        if (
            ok
            and isinstance(render_ms, int)
            and isinstance(stall_ms, int)
            and isinstance(media_ms, int)
        ):
            starts.append(render_ms)
            stall += stall_ms
            media += media_ms
        else:
            failures += 1
        rows.append(
            {
                "index": sample.get("index"),
                "ok": ok,
                "reason": reason,
                "launch": sample.get("launch"),
                "renderMs": render_ms,
                "fetchMs": fetch_ms,
                "stallMs": stall_ms,
                "mediaMs": media_ms,
            }
        )
    p95 = nearest_rank_p95(starts) if starts else None
    ratio = rebuffer_ratio(stall, media) if media else None
    return {
        "rows": rows,
        "failures": failures,
        "successes": len(starts),
        "p95_render_ms": p95,
        "stall_ms_sum": stall,
        "media_ms_sum": media,
        "rebuffer_ratio": ratio,
        "definition": "stall_ms / media_ms; p95 rank (95*n+99)//100",
    }


def run_shaped(root: Path, out: Path, content_id: str) -> int:
    ping = _run(["ping", "-c", "10", "-W", "2", "127.0.0.1"])
    parsed = _ping(ping.stdout)
    try:
        mbps: float | None = _goodput()
    except Exception as exc:
        mbps = None
        goodput_error = f"{type(exc).__name__}: {exc}"
    else:
        goodput_error = ""
    rtt = parsed[0] if parsed else None
    loss = parsed[1] if parsed else None
    ok = (
        rtt is not None
        and loss is not None
        and mbps is not None
        and shape_confirmed(rtt, mbps, loss)
    )
    body: dict[str, object] = {
        "rtt_avg_ms": rtt,
        "loss_pct": loss,
        "goodput_mbps": None if mbps is None else round(mbps, 3),
        "goodput_error": goodput_error,
        "ping": ping.stdout[-500:],
        "shape_ok": ok,
    }
    if ok:
        played = _plays(root, content_id)
        summary = _summarize(played, root)
        body["summary"] = summary
        rows = summary.get("rows")
        body["gate"] = playback_gate(
            shape_ok=True,
            sample_size=len(rows) if isinstance(rows, list) else 0,
            failures=int(cast(int, summary["failures"])),
            p95_render_ms=cast(int | None, summary["p95_render_ms"]),
            ratio=cast(float | None, summary["rebuffer_ratio"]),
        )
    else:
        body["gate"] = "BLOCKED"
        body["summary"] = {"reason": "shape band was not met; playback samples were not started"}
    out.write_text(json.dumps(body, sort_keys=True) + "\n", encoding="utf-8")
    return 0


def _sudo(*argv: str) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env["DEBIAN_FRONTEND"] = "noninteractive"
    return _run(["sudo", *argv], env=env)


def _ensure_tc() -> str:
    if shutil.which("tc") and shutil.which("ping"):
        return ""
    update = _sudo("apt-get", "update")
    install = _sudo("apt-get", "install", "-y", "iproute2", "iputils-ping")
    if install.returncode != 0 or shutil.which("tc") is None or shutil.which("ping") is None:
        return (update.stderr + install.stderr)[-500:]
    return ""


def _apply_shape() -> tuple[str, str]:
    _sudo("ip", "netns", "delete", NETNS)
    added = _sudo("ip", "netns", "add", NETNS)
    if added.returncode != 0:
        return "", added.stderr[-500:]
    steps = [
        ["ip", "netns", "exec", NETNS, "ip", "link", "set", "lo", "up"],
        [
            "ip",
            "netns",
            "exec",
            NETNS,
            "tc",
            "qdisc",
            "add",
            "dev",
            "lo",
            "root",
            "handle",
            "1:",
            "tbf",
            "rate",
            SHAPE_RATE,
            "burst",
            SHAPE_BURST,
            "latency",
            SHAPE_LATENCY,
        ],
        [
            "ip",
            "netns",
            "exec",
            NETNS,
            "tc",
            "qdisc",
            "add",
            "dev",
            "lo",
            "parent",
            "1:1",
            "handle",
            "10:",
            "netem",
            "delay",
            SHAPE_DELAY,
        ],
    ]
    for step in steps:
        proc = _sudo(*step)
        if proc.returncode != 0:
            return "", proc.stderr[-500:]
    show = _sudo("ip", "netns", "exec", NETNS, "tc", "qdisc", "show", "dev", "lo")
    return show.stdout.strip(), ""


def playback(root: Path) -> dict[str, object]:
    problem = _ensure_tc()
    if problem:
        return {"gate": "BLOCKED", "reason": "tc install failed", "detail": problem}
    shown, error = _apply_shape()
    if error:
        _sudo("ip", "netns", "delete", NETNS)
        return {
            "gate": "BLOCKED",
            "reason": "qdisc was not installed",
            "detail": error,
            "qdisc": shown,
        }
    out = root / "shaped.json"
    env = os.environ.copy()
    env["PYTHONPATH"] = os.pathsep.join(
        [
            str(ROOT / "services" / "delivery"),
            str(ROOT / "workers" / "media"),
            str(ROOT / "packages" / "protocol"),
            env.get("PYTHONPATH", ""),
        ]
    )
    env["HOME"] = "/home/ubuntu"
    env["PATH"] = os.environ.get("PATH", "/usr/bin:/bin")
    proc = subprocess.run(
        [
            "sudo",
            "ip",
            "netns",
            "exec",
            NETNS,
            "sudo",
            "-u",
            "ubuntu",
            "env",
            f"PATH={env['PATH']}",
            f"HOME={env['HOME']}",
            f"PYTHONPATH={env['PYTHONPATH']}",
            sys.executable,
            str(ROOT / "scripts" / "capacity.py"),
            "shaped",
            "--root",
            str(root),
            "--out",
            str(out),
        ],
        cwd=ROOT,
        text=True,
        capture_output=True,
        check=False,
    )
    _sudo("ip", "netns", "delete", NETNS)
    if not out.is_file():
        return {
            "gate": "BLOCKED",
            "reason": "shaped command wrote no file",
            "exit": proc.returncode,
            "tail": (proc.stdout + proc.stderr)[-2000:],
            "qdisc": shown,
        }
    body = json.loads(out.read_text(encoding="utf-8"))
    if not isinstance(body, dict):
        return {"gate": "FAIL", "reason": "bad json", "qdisc": shown}
    body["qdisc"] = shown
    has_rate = "20Mbit" in shown or "20mbit" in shown
    has_delay = "25.0ms" in shown or "25ms" in shown
    body["qdisc_has_rate"] = has_rate
    body["qdisc_has_delay"] = has_delay
    if body.get("gate") == "PASS" and not (has_rate and has_delay):
        body["gate"] = "BLOCKED"
        body["qdisc_gate"] = "the show text did not name the rate and the delay"
    return body


def _write(path: Path, body: dict[str, object]) -> None:
    path.write_text(json.dumps(body, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def run_measure(out: Path) -> int:
    root = Path("/tmp") / f"svn-goal28-{os.getpid()}"
    root.mkdir(mode=0o700)
    report: dict[str, object] = {
        "arithmetic": arithmetic(),
        "design_point": {
            "viewers_started": 0,
            "uploads_started": WORKER_CONCURRENCY,
            "gate": design_point_gate(0, WORKER_CONCURRENCY),
            "reason": (
                "no approved runner and no budget; 100 viewers and 10 uploads were not started"
            ),
        },
        "workspace": str(root),
    }
    try:
        report["hardware"] = hardware()
        _write(out, report)
        report["processing"] = processing(root)
        _write(out, report)
        report["chain"] = chain()
        _write(out, report)
        report["scheduler"] = scheduler()
        _write(out, report)
        report["playback"] = playback(root)
        _write(out, report)
    except Exception as exc:
        report["runner_error"] = f"{type(exc).__name__}: {exc}"
        _write(out, report)
        return 1
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="capacity")
    sub = parser.add_subparsers(dest="cmd", required=True)
    measure = sub.add_parser("measure")
    measure.add_argument("--out", type=Path, required=True)
    shaped = sub.add_parser("shaped")
    shaped.add_argument("--root", type=Path, required=True)
    shaped.add_argument("--out", type=Path, required=True)
    shaped.add_argument("--content", default="cminute")
    args = parser.parse_args(argv)
    if args.cmd == "measure":
        return run_measure(args.out)
    return run_shaped(args.root, args.out, args.content)


if __name__ == "__main__":
    raise SystemExit(main())
