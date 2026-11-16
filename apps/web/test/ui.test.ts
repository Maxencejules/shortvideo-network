import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import test from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { EMPTY_SETTINGS } from "../src/model.ts";
import { EMPTY_PLAYER, EMPTY_SESSION, Viewer } from "../src/ui.ts";

test("the shell is an invited build with gated social actions and no wallet control", () => {
  const html = renderToStaticMarkup(createElement(Viewer, props()));
  assert.match(html, /Invited development build/);
  assert.match(html, /does not ask for a wallet/);
  assert.match(html, /Follow is not available/);
  assert.match(html, /Block is not available/);
  assert.match(html, /Report is not available/);
  assert.match(html, /Peer sharing is off/);
  assert.match(html, /A response is not a completed watch/);
  assert.equal(html.includes("<video"), true);
  assert.equal(html.includes("disabled"), true);
  for (const page of ["Home", "Discover", "Activity", "Profile"]) assert.match(html, new RegExp(page));
});

test("captions and titles render as text", () => {
  const html = renderToStaticMarkup(
    createElement(Viewer, {
      ...props(),
      player: {
        ...EMPTY_PLAYER,
        contentId: "cportrait",
        mediaUrl: "blob:test",
        captionsOn: true,
        captionText: '<img src=x onerror="alert(1)">portrait-cue',
        sentences: ["Development fixture. This is not a live creator and not a live network statistic."],
      },
    }),
  );
  assert.equal(html.includes("<img"), false);
  assert.match(html, /&lt;img/);
  assert.match(html, /portrait-cue/);
  assert.match(html, /not a live creator/);
});

test("withdrawn content has no video element", () => {
  const html = renderToStaticMarkup(
    createElement(Viewer, {
      ...props(),
      rows: [{ contentId: "cwithdrawn", withdrawn: true, clientSelected: false }],
      player: { ...EMPTY_PLAYER, mediaUrl: "blob:should-not-render" },
    }),
  );
  assert.equal(html.includes("<video"), false);
  assert.match(html, /Withdrawn. Playback is not requested/);
});

test("discover, activity, and profile expose the gates", () => {
  const discover = renderToStaticMarkup(createElement(Viewer, { ...props(), page: "discover" }));
  assert.match(discover, /Search is not available/);
  assert.match(discover, /not a social search/);
  const activity = renderToStaticMarkup(createElement(Viewer, { ...props(), page: "activity" }));
  assert.match(activity, /Replies and notifications are not available/);
  const profile = renderToStaticMarkup(
    createElement(Viewer, {
      ...props(),
      page: "profile",
      session: { ...EMPTY_SESSION, message: "Session is missing or expired." },
      readLines: ["Indexer height 4. This is an indexer read, not a network census."],
    }),
  );
  assert.match(profile, /Session is missing or expired/);
  assert.match(profile, /Account recovery is not available/);
  assert.match(profile, /not light-client verification/);
  assert.match(profile, /not proof of storage/);
  assert.match(profile, /Reduce motion/);
});

test("source does not render user text as HTML or open a peer transport", () => {
  for (const name of readdirSync(new URL("../src/", import.meta.url))) {
    if (!name.endsWith(".ts")) continue;
    const text = readFileSync(new URL(`../src/${name}`, import.meta.url), "utf8");
    assert.equal(text.includes("dangerouslySetInnerHTML"), false, name);
    assert.equal(text.includes("innerHTML"), false, name);
    assert.equal(text.includes("RTCPeerConnection"), false, name);
    assert.equal(text.includes("26657"), false, name);
  }
});

function props() {
  return {
    page: "home" as const,
    onPage: () => undefined,
    rows: [{ contentId: "cportrait", withdrawn: false, clientSelected: true }],
    active: 0,
    onActive: () => undefined,
    player: { ...EMPTY_PLAYER, contentId: "cportrait", mediaUrl: "blob:test", qualities: [{ id: "native", label: "native 144x256", current: true }] },
    onQuality: () => undefined,
    onToggleMute: () => undefined,
    onToggleCaptions: () => undefined,
    onTogglePlay: () => undefined,
    onReplay: () => undefined,
    onSave: () => undefined,
    session: EMPTY_SESSION,
    onSignIn: () => undefined,
    onEnroll: () => undefined,
    onExport: () => undefined,
    onBackup: () => undefined,
    onDelete: () => undefined,
    readLines: ["The configured read gateway is trusted for reads. Two HTTP responses are not light-client verification."],
    settings: EMPTY_SETTINGS,
    onSettings: () => undefined,
    reducedMotion: false,
    onReducedMotion: () => undefined,
    onVideo: () => undefined,
    actionMessage: "",
  };
}
