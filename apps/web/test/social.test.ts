import assert from "node:assert/strict";
import test from "node:test";

import { commentAllowed, commentKey, parseFeed, parseHits, parseNotes } from "../src/social.ts";

test("a feed keeps stable ids and rejects a chain event", () => {
  const feed = parseFeed({
    interface_version: 1,
    kind: "following",
    chain_events: false,
    scoring_factors: ["followed creator +3"],
    items: [
      { content_id: "c-one", creator_id: "creator1", title: "harbor <b>light</b>" },
      { content_id: "Bad", creator_id: "creator1", title: "no" },
    ],
  });
  assert.deepEqual(feed.cards, [{ contentId: "c-one", creatorId: "creator1", title: "harbor light" }]);
  assert.equal(feed.factors[0], "followed creator +3");
  assert.throws(() => parseFeed({ interface_version: 1, kind: "following", chain_events: true, items: [] }));
});

test("comments and search hits are plain text", () => {
  const notes = parseNotes({
    interface_version: 1,
    comments: [{ comment_id: "m1", text: "<b>hello</b>" }],
  }, "comments", "comment_id");
  assert.equal(notes[0]?.text, "hello");
  const hits = parseHits({
    interface_version: 1,
    chain_events: false,
    hits: [{ kind: "video", content_id: "c-one", text: "harbor" }],
  });
  assert.equal(hits[0]?.id, "c-one");
  assert.equal(commentAllowed("hello"), true);
  assert.equal(commentAllowed("<img>"), false);
  assert.equal(commentKey("c-one", "hello"), commentKey("c-one", "hello"));
});
