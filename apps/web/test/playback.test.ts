import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";

import { emptyAggregates, parsePlayback, type PlaybackDocument } from "../src/model.ts";
import { PlaySession, playDocument, prefetchMasters, type ByteGet } from "../src/playback.ts";

test("playback hashes a segment, skips the rest, and does not mark a response as rendered", async () => {
  const first = new TextEncoder().encode("first-segment");
  const second = new TextEncoder().encode("second-segment");
  const third = new TextEncoder().encode("third-segment");
  const caption = new TextEncoder().encode("WEBVTT\n\n00:00.000 --> 00:00.200\nportrait-cue\n");
  const files = new Map<string, Uint8Array>([
    ["master.m3u8", text("#EXTM3U\n#EXT-X-MEDIA:TYPE=SUBTITLES,URI=\"segments/captions-0.vtt\"\n#EXT-X-STREAM-INF:BANDWIDTH=800\nnative.m3u8\n")],
    ["native.m3u8", text("#EXTM3U\n#EXTINF:1.000,\nsegments/native-0.mp4\n#EXTINF:1.000,\nsegments/native-1.mp4\n#EXTINF:1.000,\nsegments/native-2.mp4\n#EXT-X-ENDLIST\n")],
    ["segments/native-0.mp4", first],
    ["segments/native-1.mp4", second],
    ["segments/native-2.mp4", third],
    ["segments/captions-0.vtt", caption],
  ]);
  const seen: string[] = [];
  const doc = baseDoc(files);
  const metrics = emptyAggregates();
  const outcome = await playDocument(doc, fake(files, seen), new PlaySession(), metrics, null);
  assert.equal(outcome.ok, true);
  assert.equal(outcome.renditionId, "native");
  assert.equal(outcome.sha256, sha(first));
  assert.deepEqual(outcome.skipped, ["segments/native-2.mp4"]);
  assert.equal(seen.includes("segments/native-2.mp4"), false);
  assert.equal(metrics.rendered, 0);
  assert.equal(metrics.delivered_bytes, first.byteLength + second.byteLength);
  assert.equal(JSON.stringify(metrics).includes("cportrait"), false);
});

test("a dead host falls back and a bad hash is not played", async () => {
  const body = new TextEncoder().encode("good-bytes");
  const files = new Map<string, Uint8Array>([
    ["master.m3u8", text("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800\nnative.m3u8\n")],
    ["native.m3u8", text("#EXTM3U\n#EXTINF:1.000,\nsegments/native-0.mp4\n#EXT-X-ENDLIST\n")],
    ["segments/native-0.mp4", body],
  ]);
  const doc = baseDoc(files);
  doc.captions = null;
  doc.hosts = [
    { host_id: "test-a", role: "test", base_url: "https://127.0.0.1:9", verified: true },
    { host_id: "origin-test", role: "origin", base_url: "https://127.0.0.1:8", verified: true },
  ];
  const get: ByteGet = async (url) => {
    if (url.startsWith("https://127.0.0.1:9/")) return { status: 500, body: new Uint8Array() };
    const name = url.slice("https://127.0.0.1:8/".length);
    const found = files.get(name);
    if (!found) return { status: 404, body: new Uint8Array() };
    return { status: 200, body: found };
  };
  const outcome = await playDocument(doc, get, new PlaySession(), emptyAggregates(), null);
  assert.equal(outcome.ok, true);
  assert.equal(outcome.hostsTried[0], "test-a");
  assert.ok(outcome.hostsTried.includes("origin-test"));
  const broken = new Map(files);
  broken.set("segments/native-0.mp4", new TextEncoder().encode("tampered-bytes"));
  const bad = await playDocument(doc, async (url) => {
    const name = url.startsWith("https://127.0.0.1:9/") ? url.slice("https://127.0.0.1:9/".length) : url.slice("https://127.0.0.1:8/".length);
    const found = broken.get(name);
    return found ? { status: 200, body: found } : { status: 404, body: new Uint8Array() };
  }, new PlaySession(), emptyAggregates(), null);
  assert.equal(bad.ok, false);
  assert.equal(bad.error, "INTEGRITY");
  assert.equal(bad.bytes, null);
});

test("cancel stops a hanging fetch and prefetch does not download media", async () => {
  const files = new Map<string, Uint8Array>([
    ["master.m3u8", text("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800\nnative.m3u8\n")],
    ["native.m3u8", text("#EXTM3U\n#EXTINF:1.000,\nsegments/native-0.mp4\n#EXT-X-ENDLIST\n")],
    ["segments/native-0.mp4", new TextEncoder().encode("x")],
  ]);
  const doc = baseDoc(files);
  doc.captions = null;
  const session = new PlaySession();
  const get: ByteGet = (_url, signal) =>
    new Promise((_resolve, reject) => {
      signal.addEventListener("abort", () => reject(Object.assign(new Error("CANCELLED"), { code: "CANCELLED" })));
    });
  const pending = playDocument(doc, get, session, emptyAggregates(), null);
  session.cancel();
  const outcome = await pending;
  assert.equal(outcome.error, "CANCELLED");
  const seen: string[] = [];
  const metrics = emptyAggregates();
  const skipped = await prefetchMasters([doc, doc, doc], fake(files, seen), new PlaySession(), metrics);
  assert.equal(skipped, 2);
  assert.deepEqual(seen, ["master.m3u8"]);
  assert.equal(metrics.delivered_bytes, 0);
});

function text(value: string): Uint8Array {
  return new TextEncoder().encode(value);
}

function sha(bytes: Uint8Array): string {
  return createHash("sha256").update(bytes).digest("hex");
}

function fake(files: Map<string, Uint8Array>, seen: string[]): ByteGet {
  return async (url) => {
    const name = url.slice(url.indexOf("/", url.indexOf("://") + 3) + 1);
    seen.push(name);
    const body = files.get(name);
    return body ? { status: 200, body } : { status: 404, body: new Uint8Array() };
  };
}

function baseDoc(files: Map<string, Uint8Array>): PlaybackDocument {
  const segment = files.get("segments/native-0.mp4") ?? new Uint8Array();
  const second = files.get("segments/native-1.mp4");
  const third = files.get("segments/native-2.mp4");
  const caption = files.get("segments/captions-0.vtt");
  const segments = [{ segment_id: "native-0", sha256: sha(segment), byte_count: String(segment.byteLength), path: "segments/native-0.mp4" }];
  if (second) segments.push({ segment_id: "native-1", sha256: sha(second), byte_count: String(second.byteLength), path: "segments/native-1.mp4" });
  if (third) segments.push({ segment_id: "native-2", sha256: sha(third), byte_count: String(third.byteLength), path: "segments/native-2.mp4" });
  return parsePlayback({
    schema_version: 1,
    adapter: "origin-test",
    content_id: "cportrait",
    publication: "NOT_PUBLISHED",
    removal: "NONE",
    playback_grant: "AUTHORIZED",
    chain_status: "HELD",
    stale: false,
    hosting_promise: false,
    package_verified: true,
    hosts: [{ host_id: "origin-test", role: "origin", base_url: "https://127.0.0.1:9", verified: true }],
    disabled: ["ipfs", "p2p", "turn", "webrtc"],
    master_path: "master.m3u8",
    fallback_rendition: "native",
    renditions: [{ rendition_id: "native", bandwidth: 800, width: 144, height: 256, codecs: "avc1", segments }],
    captions: caption
      ? { path: "segments/captions-0.vtt", sha256: sha(caption), byte_count: String(caption.byteLength) }
      : null,
  });
}
