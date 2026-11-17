import assert from "node:assert/strict";
import test from "node:test";

import { readingText, storageForcesValidator, toggleDuty, visibleActions } from "../src/operator.ts";

test("storage does not select validator", () => {
  const selected = toggleDuty([], "storage_delivery");
  assert.deepEqual(selected, ["storage_delivery"]);
  assert.equal(storageForcesValidator(selected), false);
  assert.equal(selected.includes("validator"), false);
});

test("duties toggle independently", () => {
  const both = toggleDuty(toggleDuty([], "validator"), "processing");
  assert.deepEqual(both, ["validator", "processing"]);
  assert.deepEqual(toggleDuty(both, "validator"), ["processing"]);
});

test("a stale reading stays marked stale", () => {
  const text = readingText({
    name: "index_height",
    class: "stale",
    value: 4,
    source: "indexer",
    note: "stale: commit_age",
  });
  assert.equal(text.includes("STALE"), true);
  assert.equal(text.includes("4"), true);
  assert.equal(text.includes("commit_age"), true);
});

test("a reported zero stays measured", () => {
  const text = readingText({ name: "used_bytes", class: "measured", value: 0, source: "provider" });
  assert.equal(text, "used_bytes measured 0");
});

test("roles see only their actions", () => {
  assert.deepEqual(visibleActions("viewer"), []);
  assert.equal(visibleActions("provider").includes("drain"), true);
  assert.equal(visibleActions("provider").includes("queue"), false);
  assert.deepEqual(visibleActions("moderator"), ["queue"]);
  assert.deepEqual(visibleActions("council"), ["governance"]);
  assert.equal(visibleActions("council").includes("drain"), false);
});
