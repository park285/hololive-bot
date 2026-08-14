import assert from "node:assert/strict";
import test from "node:test";

import { paginate, paginationResult } from "./pagination.mjs";

function page(id, continuation) {
  return { id, continuation, items: [{ id }] };
}

test("paginate marks a single exhausted page COMPLETE/CONTIGUOUS", async () => {
  const result = await paginate({
    firstPage: page("a"),
    getContinuation: async () => {
      throw new Error("should not continue");
    },
    mapPage: (current) => current.items,
    maxPages: 3,
  });
  assert.equal(result.page_count, 1);
  assert.equal(result.exhausted, true);
  assert.equal(result.continuity, "CONTIGUOUS");
  assert.deepEqual(result.items, [{ id: "a" }]);
});

test("paginate preserves a validated prefix after a later page timeout", async () => {
  const result = await paginate({
    firstPage: page("a", "cursor-b"),
    getContinuation: async () => {
      const err = new Error("timed out");
      err.code = "collection_timeout";
      throw err;
    },
    mapPage: (current) => current.items,
    maxPages: 3,
  });
  assert.equal(result.page_count, 1);
  assert.equal(result.exhausted, false);
  assert.equal(result.continuity, "GAP_UNRESOLVED");
  assert.deepEqual(result.items, [{ id: "a" }]);
});

test("paginate treats a cursor loop as PARTIAL + GAP_UNRESOLVED", async () => {
  const result = await paginate({
    firstPage: page("a", "loop"),
    getContinuation: async () => page("b", "loop"),
    mapPage: (current) => current.items,
    maxPages: 5,
  });
  assert.equal(result.page_count, 2);
  assert.equal(result.exhausted, false);
  assert.equal(result.continuity, "GAP_UNRESOLVED");
  assert.deepEqual(
    result.items.map((item) => item.id),
    ["a", "b"],
  );
});

test("paginate stops at max pages without claiming exhaustion", async () => {
  const result = await paginate({
    firstPage: page("a", "b"),
    getContinuation: async (current) => page(current.continuation, "c"),
    mapPage: (current) => current.items,
    maxPages: 1,
  });
  assert.equal(result.page_count, 1);
  assert.equal(result.exhausted, false);
  assert.equal(result.continuity, "GAP_UNRESOLVED");
});

test("paginate stops at max aggregate bytes and keeps the validated prefix", async () => {
  const result = await paginate({
    firstPage: { items: [{ id: "tiny" }, { id: "too-large-item" }] },
    getContinuation: async () => {
      throw new Error("should not continue");
    },
    mapPage: (current) => current.items,
    maxPages: 2,
    maxAggregateBytes: 20,
  });
  assert.equal(result.items.length, 1);
  assert.equal(result.items[0].id, "tiny");
  assert.equal(result.exhausted, false);
  assert.equal(result.continuity, "GAP_UNRESOLVED");
});

test("paginate fail-closes on first-page mapper drift", async () => {
  await assert.rejects(
    () =>
      paginate({
        firstPage: page("a"),
        getContinuation: async () => page("b"),
        mapPage: () => {
          const err = new Error("schema drifted");
          err.code = "parser_drift";
          throw err;
        },
        maxPages: 2,
      }),
    (err) => err.code === "parser_drift",
  );
});

test("paginate fail-closes when continuation reports parser drift", async () => {
  await assert.rejects(
    () =>
      paginate({
        firstPage: page("a", "b"),
        getContinuation: async () => {
          const err = new Error("schema drifted");
          err.code = "parser_drift";
          throw err;
        },
        mapPage: (current) => current.items,
        maxPages: 3,
      }),
    (err) => err.code === "parser_drift",
  );
});

test("paginationResult does not mark truncated pages exhausted", () => {
  const result = paginationResult({
    pageCount: 2,
    cursorStart: "a",
    cursorEnd: "b",
    exhausted: true,
    truncated: true,
  });
  assert.equal(result.exhausted, false);
  assert.equal(result.continuity, "GAP_UNRESOLVED");
});
