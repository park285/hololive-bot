import assert from "node:assert/strict";
import test from "node:test";

import { createRealFetchers } from "./real-fetchers.mjs";

test("rejected Innertube init clears the promise", async () => {
  let calls = 0;
  const fetchers = createRealFetchers({
    createInnertubeImpl: async () => {
      calls += 1;
      throw new Error("init failed");
    },
  });
  await assert.rejects(() => fetchers.fetchViewer({ videoId: "vid-1" }), /init failed/);
  await assert.rejects(() => fetchers.fetchViewer({ videoId: "vid-1" }), /init failed/);
  assert.equal(calls, 2);
});

test("concurrent Innertube init shares one promise", async () => {
  let calls = 0;
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  const fetchers = createRealFetchers({
    createInnertubeImpl: async () => {
      calls += 1;
      await gate;
      return { getChannel() {} };
    },
  });
  const first = fetchers.fetchViewer({ videoId: "vid-1" });
  const second = fetchers.fetchViewer({ videoId: "vid-2" });
  release();
  await Promise.allSettled([first, second]);
  assert.equal(calls, 1);
});
