"""Headless playback page. A dump of this page is not a decode unless the browser ran it.

Chromium on Linux has no native HLS demuxer. The page fetches the playlist,
hashes the one media segment it fetched, and decodes that MP4 in a video
element. Native HLS segment verification is not claimed.
"""

from __future__ import annotations

import json
import shutil
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
    """Start a headless browser. Return ABSENT, REPORTED, or FAILED.

    REPORTED means `ready` returned a document. It does not, by itself, mean
    the video decoded. The caller checks width, height, and the hash.
    """
    binary = find_browser()
    if binary is None:
        return "ABSENT"
    with tempfile.TemporaryDirectory(prefix="svn-chrome-") as folder:
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
            process = subprocess.Popen(
                command,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
        except OSError:
            return "FAILED"
        deadline = time.monotonic() + timeout
        try:
            while time.monotonic() < deadline:
                if ready() is not None:
                    return "REPORTED"
                if process.poll() is not None:
                    return "FAILED"
                time.sleep(0.2)
            return "FAILED"
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
