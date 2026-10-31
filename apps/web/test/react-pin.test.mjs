import assert from "node:assert/strict";
import test from "node:test";
import { version } from "react";

test("react version is the pinned release", () => {
  assert.equal(version, "19.3.1");
});
