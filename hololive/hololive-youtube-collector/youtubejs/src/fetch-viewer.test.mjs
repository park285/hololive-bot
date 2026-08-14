import assert from "node:assert/strict";
import test from "node:test";

import { mapViewer } from "./fetch-viewer.mjs";

test("mapViewer keeps a hidden count typed and non-zero", () => {
  const result = mapViewer({ basic_info: { hidden_view_count: true } }, "vid-1");
  assert.equal(result.video_id, "vid-1");
  assert.equal(result.viewer_count, null);
  assert.equal(result.availability, "HIDDEN");
});

test("mapViewer keeps a missing count unavailable instead of zero", () => {
  const result = mapViewer({ basic_info: {} }, "vid-2");
  assert.equal(result.viewer_count, null);
  assert.equal(result.availability, "UNAVAILABLE");
});

test("mapViewer returns an available count", () => {
  const result = mapViewer({ basic_info: { id: "vid-3", view_count: 42 } }, "vid-3");
  assert.equal(result.viewer_count, 42);
  assert.equal(result.availability, "AVAILABLE");
});

test("mapViewer fail-closes on a negative count", () => {
  assert.throws(
    () => mapViewer({ basic_info: { view_count: -1 } }, "vid-4"),
    (err) => err.code === "parser_drift",
  );
});
