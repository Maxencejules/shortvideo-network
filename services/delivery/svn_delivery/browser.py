"""Headless playback page. A dump of this page is not a decode unless the browser ran it.

Chromium on Linux has no native HLS demuxer. The page fetches the playlist,
hashes the one media segment it fetched, and decodes that MP4 in a video
element. Native HLS segment verification is not claimed.
"""

from __future__ import annotations

import json
import os
import shutil
import signal
import subprocess
import tempfile
import time
from collections.abc import Callable

from svn_delivery.errors import DeliveryError
from svn_delivery.ids import ID

_PAGE = """<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>svn playback</title></head>
<body><pre id="svn-result">pending</pre>
<script>
const contentId = __CONTENT_ID__;
async function sha256(buffer) {
  const digest = await crypto.subtle.digest("SHA-256", buffer);
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}
function rel(base, ref) {
  if (!ref || ref.includes("..") || ref.startsWith("/") || ref.includes("://")) {
    throw new Error("relative");
  }
  return base.replace(/\\/$/, "") + "/" + ref;
}
async function main() {
  const out = {
    rendered:false, caption:false, error:"", sha256:"",
    videoWidth:0, videoHeight:0, readyState:0, nonzero:0
  };
  try {
    const doc = await (await fetch("/v1/playback/" + contentId)).json();
    if (doc.hosting_promise !== false || !doc.hosts || !doc.hosts.length) {
      throw new Error("no-host");
    }
    const base = doc.hosts[0].base_url;
    const master = await (await fetch(rel(base, "master.m3u8"))).text();
    const lines = master.split("\\n").map((l) => l.trim()).filter((l) => l && !l.startsWith("#"));
    const variant = lines[lines.length - 1];
    const media = await (await fetch(rel(base, variant))).text();
    const mediaLines = media.split("\\n").map((l) => l.trim());
    const segment = mediaLines.filter((l) => l && !l.startsWith("#"))[0];
    const bytes = await (await fetch(rel(base, segment))).arrayBuffer();
    out.sha256 = await sha256(bytes);
    const video = document.createElement("video");
    video.muted = true;
    video.playsInline = true;
    video.src = URL.createObjectURL(new Blob([bytes], {type: "video/mp4"}));
    document.body.appendChild(video);
    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error("timeout")), 12000);
      video.onloadeddata = () => { clearTimeout(timer); resolve(); };
      video.onerror = () => { clearTimeout(timer); reject(new Error("decode")); };
      video.play().catch(() => {});
    });
    if (video.requestVideoFrameCallback) {
      await new Promise((resolve) => {
        const timer = setTimeout(resolve, 2000);
        video.requestVideoFrameCallback(() => { clearTimeout(timer); resolve(); });
      });
    }
    out.readyState = video.readyState;
    out.videoWidth = video.videoWidth;
    out.videoHeight = video.videoHeight;
    const canvas = document.createElement("canvas");
    canvas.width = video.videoWidth;
    canvas.height = video.videoHeight;
    const ctx = canvas.getContext("2d");
    ctx.drawImage(video, 0, 0);
    const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height).data;
    let nonzero = 0;
    for (let i = 0; i < pixels.length; i += 4) {
      if (pixels[i] || pixels[i + 1] || pixels[i + 2]) nonzero += 1;
    }
    out.nonzero = nonzero;
    if (doc.captions && doc.captions.path) {
      const text = await (await fetch(rel(base, doc.captions.path))).text();
      out.caption = text.startsWith("WEBVTT");
    }
    out.rendered = video.readyState >= 2 && video.videoWidth > 0 && nonzero > 0;
  } catch (err) {
    out.error = String(err && err.message || err).slice(0, 200);
  }
  document.getElementById("svn-result").textContent = JSON.stringify(out);
  await fetch("/v1/player/" + contentId + "/report", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify(out)
  });
}
main();
</script></body></html>
"""


def page_html(content_id: str) -> str:
    if not ID.fullmatch(content_id):
        raise DeliveryError("BAD_REQUEST")
    return _PAGE.replace("__CONTENT_ID__", json.dumps(content_id))


_CAPACITY_PAGE = """<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>svn capacity</title></head>
<body><pre id="svn-result">pending</pre>
<script>
const contentId = __CONTENT_ID__;
function rel(base, ref) {
  if (!ref || ref.includes("..") || ref.startsWith("/") || ref.includes("://")) {
    throw new Error("relative");
  }
  return base.replace(/\\/$/, "") + "/" + ref;
}
async function sha256(buffer) {
  const digest = await crypto.subtle.digest("SHA-256", buffer);
  const bytes = new Uint8Array(digest);
  return [...bytes].map((b) => b.toString(16).padStart(2, "0")).join("");
}
async function main() {
  const started = performance.now();
  const out = {
    rendered: false, caption: false, error: "", sha256: "",
    videoWidth: 0, videoHeight: 0, readyState: 0, nonzero: 0,
    renderMs: 0, fetchMs: 0, stallMs: 0, mediaMs: 0, segment: ""
  };
  let renderedAt = 0;
  let stallMark = 0;
  let stall = 0;
  function addStall() {
    if (!stallMark) return;
    stall += performance.now() - stallMark;
    stallMark = 0;
  }
  try {
    const doc = await (await fetch("/v1/playback/" + contentId)).json();
    if (doc.hosting_promise !== false || !doc.hosts || !doc.hosts.length) {
      throw new Error("no-host");
    }
    const base = doc.hosts[0].base_url;
    const masterText = await (await fetch(rel(base, "master.m3u8"))).text();
    const masterLines = masterText.split("\\n").map((line) => line.trim());
    const variants = masterLines.filter((line) => line && !line.startsWith("#"));
    const variant = variants[0];
    if (!variant) throw new Error("variant");
    const mediaText = await (await fetch(rel(base, variant))).text();
    const mediaLines = mediaText.split("\\n").map((line) => line.trim());
    const segment = mediaLines.filter((line) => line && !line.startsWith("#"))[0];
    if (!segment) throw new Error("segment");
    out.segment = segment;
    const fetchStart = performance.now();
    const payload = await (await fetch(rel(base, segment))).arrayBuffer();
    out.fetchMs = Math.round(performance.now() - fetchStart);
    out.sha256 = await sha256(payload);
    const video = document.createElement("video");
    video.muted = true;
    video.playsInline = true;
    const blob = new Blob([payload], {type: "video/mp4"});
    video.src = URL.createObjectURL(blob);
    document.body.appendChild(video);
    video.addEventListener("waiting", () => {
      if (renderedAt && !stallMark) stallMark = performance.now();
    });
    video.addEventListener("playing", addStall);
    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error("timeout")), 20000);
      video.onloadeddata = () => { clearTimeout(timer); resolve(); };
      video.onerror = () => { clearTimeout(timer); reject(new Error("decode")); };
      video.play().catch(() => {});
    });
    if (video.requestVideoFrameCallback) {
      await new Promise((resolve) => {
        const timer = setTimeout(resolve, 2000);
        video.requestVideoFrameCallback(() => { clearTimeout(timer); resolve(); });
      });
    }
    out.readyState = video.readyState;
    out.videoWidth = video.videoWidth;
    out.videoHeight = video.videoHeight;
    const canvas = document.createElement("canvas");
    canvas.width = video.videoWidth;
    canvas.height = video.videoHeight;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("canvas");
    ctx.drawImage(video, 0, 0);
    const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height).data;
    let nonzero = 0;
    for (let i = 0; i < pixels.length; i += 4) {
      if (pixels[i] || pixels[i + 1] || pixels[i + 2]) nonzero += 1;
    }
    out.nonzero = nonzero;
    const drew = video.readyState >= 2 && video.videoWidth > 0 && nonzero > 0;
    out.rendered = drew;
    if (drew) renderedAt = performance.now();
    out.renderMs = Math.round(drew ? renderedAt - started : 0);
    await new Promise((resolve) => {
      const timer = setTimeout(() => resolve(), 80000);
      video.onended = () => { clearTimeout(timer); resolve(); };
    });
    addStall();
    out.stallMs = Math.round(stall);
    out.mediaMs = Math.round((video.currentTime || 0) * 1000);
  } catch (err) {
    out.error = String((err && err.message) || err).slice(0, 200);
  }
  const node = document.getElementById("svn-result");
  if (node) node.textContent = JSON.stringify(out);
  await fetch("/v1/player/" + contentId + "/report", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify(out)
  });
}
main();
</script></body></html>
"""


def capacity_page_html(content_id: str) -> str:
    """Play the first master variant through to the end.

    `apps/web/src/playback.ts` selects that first variant. Stall time starts
    only after a decoded frame. A download without that frame is not success.
    """
    if not ID.fullmatch(content_id):
        raise DeliveryError("BAD_REQUEST")
    return _CAPACITY_PAGE.replace("__CONTENT_ID__", json.dumps(content_id))


def _stop_browser(process: subprocess.Popen[bytes]) -> None:
    """Stop the browser and the children that keep its profile directory open."""
    if process.poll() is None:
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except OSError:
            process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except OSError:
        return


def find_browser() -> str | None:
    for name in ("google-chrome", "google-chrome-stable", "chromium", "chromium-browser"):
        found = shutil.which(name)
        if found:
            return found
    return None


def run_until(
    url: str,
    ready: Callable[[], dict[str, object] | None],
    *,
    timeout: float = 25,
) -> str:
    """Start a headless browser. Return ABSENT, REPORTED, EXITED, LAUNCH_FAILED, or TIMEOUT.

    REPORTED means `ready` returned a document. It does not, by itself, mean
    the video decoded. The caller checks width, height, and the hash.
    EXITED means the process started and then ended before that document existed.
    LAUNCH_FAILED means the process never started. A failed launch is not an early exit.
    TIMEOUT means the process was still running at the deadline and `ready`
    was still empty. A timeout is not an early exit.
    """
    binary = find_browser()
    if binary is None:
        return "ABSENT"
    folder = tempfile.mkdtemp(prefix="svn-chrome-")
    command = [
        binary,
        "--headless=new",
        "--disable-gpu",
        "--no-sandbox",
        "--disable-dev-shm-usage",
        "--ignore-certificate-errors",
        "--allow-insecure-localhost",
        "--autoplay-policy=no-user-gesture-required",
        "--no-first-run",
        "--no-default-browser-check",
        f"--user-data-dir={folder}",
        "--window-size=640,480",
        url,
    ]
    try:
        try:
            process = subprocess.Popen(
                command,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                start_new_session=True,
            )
        except OSError:
            return "LAUNCH_FAILED"
        deadline = time.monotonic() + timeout
        try:
            while time.monotonic() < deadline:
                if ready() is not None:
                    return "REPORTED"
                if process.poll() is not None:
                    return "EXITED"
                time.sleep(0.2)
            return "TIMEOUT"
        finally:
            _stop_browser(process)
    finally:
        shutil.rmtree(folder, ignore_errors=True)
