import assert from "node:assert/strict";
import test from "node:test";

import {
  ApiError,
  activeLeaseCount,
  assertNoSecretQuery,
  blockSentence,
  buildFeed,
  emptyAggregates,
  parseContentList,
  parsePlayback,
  parseStatus,
  parseVtt,
  plainText,
  readSettings,
  settingsRecord,
  statusSentences,
} from "../src/model.ts";

test("plain text drops markup", () => {
  assert.equal(plainText('<img src=x onerror="alert(1)">hello'), "hello");
  assert.equal(plainText("a < c"), "a c");
});

test("captions parse as text", () => {
  const cues = parseVtt("WEBVTT\n\n00:00.000 --> 00:00.200\nportrait-cue\n");
  assert.equal(cues.length, 1);
  assert.equal(cues[0]?.text, "portrait-cue");
});

test("withdrawn and quarantined rows are not playable", () => {
  const rows = parseContentList({
    content: [
      { ID: "cportrait", Version: 1, Manifest: "a", PackageBytes: "1", Withdrawn: false, HistoryErased: false },
      { ID: "cwithdrawn", Version: 1, Manifest: "b", PackageBytes: "1", Withdrawn: true, HistoryErased: false },
    ],
  });
  const feed = buildFeed(rows, ["cportrait", "cwithdrawn", "crotated"]);
  assert.deepEqual(
    feed.map((row) => [row.contentId, row.withdrawn]),
    [
      ["cportrait", false],
      ["crotated", false],
      ["cwithdrawn", true],
    ],
  );
  const blocked = feed.find((row) => row.contentId === "cwithdrawn");
  assert.ok(blocked);
  assert.equal(blockSentence(blocked, null), "Withdrawn. Playback is not requested.");
});

test("a content row without a withdrawn flag is refused", () => {
  assert.throws(() => parseContentList({ content: [{ ID: "cportrait", Version: 1 }] }), ApiError);
});

test("playback document drops a non-loopback or quarantine host", () => {
  const doc = parsePlayback(document([{ host_id: "origin-test", role: "origin", base_url: "https://127.0.0.1:9/quarantine", verified: true }]));
  assert.equal(doc.hosts.length, 0);
  assert.equal(blockSentence({ contentId: "cportrait", withdrawn: false, clientSelected: false }, doc), "Unavailable.");
});

test("a hosting promise is refused", () => {
  assert.throws(() => parsePlayback({ ...document([]), hosting_promise: true }), ApiError);
});

test("stale and degraded sentences stay visible", () => {
  const doc = parsePlayback(document([]));
  doc.stale = true;
  doc.chain_status = "STALE";
  doc.package_verified = true;
  doc.playback_grant = "AUTHORIZED";
  doc.hosts = [{ host_id: "origin-test", role: "origin", base_url: "https://127.0.0.1:9", verified: true }];
  const lines = statusSentences(doc, activeLeaseCount([{ ID: "l", ProviderID: "p", ContentID: "cportrait", Active: true }, { ID: "m", ProviderID: "q", ContentID: "cportrait", Active: true }], "cportrait"));
  assert.ok(lines.includes("Chain data is stale."));
  assert.ok(lines.includes("Availability degraded."));
  assert.ok(lines.includes("Not published. A local playback grant is not publication."));
  assert.ok(lines.some((line) => line.includes("not a live creator")));
});

test("status reads do not treat two responses as a light client", () => {
  const status = parseStatus({
    chain_id: "devnet",
    indexed_height: 4,
    stale: false,
    trust: { light_client: false, note: "The configured read gateway is trusted for reads." },
  });
  assert.equal(status.lightClient, false);
  assert.equal(status.indexedHeight, 4);
});

test("settings reject a public host and a secret query", () => {
  assert.throws(() => readSettings({ deliveryBase: "https://cdn.example/v1" }), ApiError);
  assert.throws(() => assertNoSecretQuery("https://127.0.0.1:9/v1/session?token=abc"), ApiError);
  const saved = settingsRecord(readSettings({ deliveryBase: "https://127.0.0.1:9", contentIds: ["cportrait", "Bad Id"] }));
  assert.equal(saved.includes("token"), false);
  assert.equal(saved.includes("cportrait"), true);
  assert.equal(saved.includes("Bad"), false);
});

test("aggregate counters have no content id", () => {
  const exported = emptyAggregates();
  assert.equal(JSON.stringify(exported).includes("cportrait"), false);
  assert.equal("content_id" in exported, false);
});

function document(hosts: unknown[]): Record<string, unknown> {
  return {
    schema_version: 1,
    adapter: "origin-test",
    content_id: "cportrait",
    publication: "NOT_PUBLISHED",
    removal: "NONE",
    playback_grant: "AUTHORIZED",
    chain_status: "HELD",
    stale: false,
    hosting_promise: false,
    package_verified: false,
    hosts,
    disabled: ["ipfs", "p2p", "turn", "webrtc"],
    master_path: null,
    fallback_rendition: null,
    renditions: [],
    captions: null,
  };
}
