import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import test from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { composeCreator, emptyDraft, hostingTerms, analyticsLines, publicSnapshot, RECOVERY_TEXT } from "../src/creator.ts";
import { EMPTY_SETTINGS, emptyAggregates } from "../src/model.ts";
import { EMPTY_PLAYER, EMPTY_SESSION, Viewer, type CreatorPanel } from "../src/ui.ts";

test("the shell is an invited build with gated social actions and no wallet control", () => {
  const html = renderToStaticMarkup(createElement(Viewer, props()));
  assert.match(html, /Invited development build/);
  assert.match(html, /does not ask for a wallet/);
  assert.match(html, /A follow is stored for this account/);
  assert.match(html, /not a chain transaction/);
  assert.match(html, /Block is not available/);
  assert.match(html, /Report is not available/);
  assert.match(html, /Comments are plain text/);
  assert.match(html, /moderator queue is not implemented/);
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

test("the mute button names the state it will apply", () => {
  const quiet = renderToStaticMarkup(
    createElement(Viewer, { ...props(), player: { ...EMPTY_PLAYER, contentId: "cportrait", mediaUrl: "blob:test", muted: true } }),
  );
  const audible = renderToStaticMarkup(
    createElement(Viewer, { ...props(), player: { ...EMPTY_PLAYER, contentId: "cportrait", mediaUrl: "blob:test", muted: false } }),
  );
  assert.match(quiet, /id="mute"/);
  assert.match(quiet, />Unmute</);
  assert.equal(audible.includes(">Unmute<"), false);
  assert.match(audible, />Mute</);
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

test("discover, activity, and profile expose search, replies, and the moderation gates", () => {
  const discover = renderToStaticMarkup(createElement(Viewer, {
    ...props(),
    page: "discover",
    social: {
      feedKind: "recommended",
      factors: [],
      comments: [{ id: "m1", text: '<img src=x onerror="alert(1)">' }],
      activity: [],
      hits: [{ id: "cportrait", kind: "video", text: "harbor" }],
      onFeedKind: () => undefined,
      onSearch: () => undefined,
    },
  }));
  assert.match(discover, /Search reads permitted profiles, topics, and videos/);
  assert.equal(discover.includes("Search is not available"), false);
  assert.equal(discover.includes("<img"), false);
  assert.match(discover, /harbor/);
  const activity = renderToStaticMarkup(createElement(Viewer, {
    ...props(),
    page: "activity",
    social: {
      feedKind: "following",
      factors: ["followed creator +3"],
      comments: [],
      activity: [{ id: "n1", text: "hello" }],
      hits: [],
      onFeedKind: () => undefined,
    },
  }));
  assert.match(activity, /hello/);
  assert.equal(activity.includes("Replies and notifications are not available"), false);
  const home = renderToStaticMarkup(createElement(Viewer, {
    ...props(),
    social: {
      feedKind: "recommended",
      factors: ["followed creator +3"],
      comments: [{ id: "m1", text: '<img src=x onerror="alert(1)">' }],
      activity: [],
      hits: [],
      onFeedKind: () => undefined,
      onComment: () => undefined,
    },
  }));
  assert.match(home, /Ranking factors/);
  assert.match(home, /not a measured improvement/);
  assert.equal(home.includes("<img"), false);
  assert.match(home, /&lt;img/);
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

test("the create page keeps stages separate and does not publish from a draft", () => {
  const draft = emptyDraft("acct");
  const state = composeCreator({
    ownerAccountId: draft.ownerAccountId,
    sessionAccountId: "acct",
    optimisticPublished: true,
    upload: null,
    availability: null,
    chain: null,
    indexerStale: false,
    withdrawn: false,
    activeLeaseProviders: [],
    removalRequested: false,
    mediaStatus: "",
    playbackPublication: "",
  });
  const html = renderToStaticMarkup(
    createElement(Viewer, {
      ...props(),
      page: "create",
      session: { signedIn: true, accountId: "acct", role: "creator", message: "" },
      creator: panel(state, "<img src=x>"),
    }),
  );
  assert.equal(html.includes("<img"), false);
  assert.match(html, /&lt;img/);
  assert.match(html, /id="creator-stage"[^>]*>Draft</);
  assert.equal(html.includes('aria-current="step"'), false);
  for (const name of ["Uploading", "Processing", "Review pending", "Replicating", "Published"]) {
    assert.equal(html.includes(`<li>${name}</li>`), true, name);
  }
  assert.match(html, new RegExp(RECOVERY_TEXT.replace(/[.]/g, "\\.")));
  assert.match(html, /Another frame/);
  assert.match(html, /disabled/);
  assert.match(html, /not an approved allowance/);
  assert.match(html, /not views, not revenue/);
  assert.match(html, /local publish flag is ignored/);
  assert.equal(html.includes("chain certified this video as lawful or truthful.</"), false);
});

test("published copy names the conditions and not a certification", () => {
  const state = composeCreator({
    ownerAccountId: "acct",
    sessionAccountId: "acct",
    optimisticPublished: true,
    upload: null,
    availability: {
      content_id: "c".padEnd(33, "a"),
      publication: "PUBLISHED",
      publication_reason: "",
      community_approval: true,
      reachability: "REACHABLE",
      at_risk: false,
      renewal: "NONE",
      degraded_providers: [],
      simulated_labels: false,
      independent_operator_verification: false,
      storage_proof: false,
      active_commitments: 3,
      verified_transfers: 3,
    },
    chain: { status: "OPERATION_STATUS_COMMITTED" },
    indexerStale: false,
    withdrawn: false,
    activeLeaseProviders: [],
    removalRequested: false,
    mediaStatus: "READY_FOR_REVIEW",
    playbackPublication: "NOT_PUBLISHED",
  });
  assert.equal(state.stage, "Published");
  assert.equal(state.published, true);
  const html = renderToStaticMarkup(createElement(Viewer, { ...props(), page: "create", creator: panel(state, "clip") }));
  assert.match(html, /does not mean the chain certified this video as lawful or truthful/);
  assert.match(html, /Goal 23 moderation completion is not this screen/);
  assert.match(html, /playback document is not the publication record/);
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
    creator: panel(composeCreator({
      ownerAccountId: "",
      sessionAccountId: "",
      optimisticPublished: false,
      upload: null,
      availability: null,
      chain: null,
      indexerStale: false,
      withdrawn: false,
      activeLeaseProviders: [],
      removalRequested: false,
      mediaStatus: "",
      playbackPublication: "",
    }), ""),
    onCreatorField: () => undefined,
    onCreatorFile: () => undefined,
    onCreatorAction: () => undefined,
  };
}

function panel(state: ReturnType<typeof composeCreator>, title: string): CreatorPanel {
  return {
    state,
    snapshot: publicSnapshot(state, ""),
    fields: { title, description: "", captions: "", trimStart: "0", trimEnd: "1", confirm: "" },
    fileName: "",
    message: "",
    terms: hostingTerms(null),
    analytics: analyticsLines(emptyAggregates()),
    videos: [],
  };
}
