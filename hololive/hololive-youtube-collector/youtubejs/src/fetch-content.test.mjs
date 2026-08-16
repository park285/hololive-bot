import assert from "node:assert/strict";
import test from "node:test";

import { fetchContentFeed, mapContentItems } from "./fetch-content.mjs";

test("mapContentItems fail-closes when a row is missing video id", () => {
  assert.throws(
    () =>
      mapContentItems(
        { videos: [{ id: "vid-1", title: "One" }, { title: "missing" }] },
        "UC_TEST",
      ),
    (err) => err.code === "parser_drift",
  );
});

test("mapContentItems maps current YouTube.js LockupView rows", () => {
  const items = mapContentItems(
    { videos: [{ type: "LockupView", content_type: "VIDEO", content_id: "video-1", metadata: { title: "Title" } }] },
    "UC_TEST",
  );
  assert.equal(items[0].video_id, "video-1");
  assert.equal(items[0].title, "Title");
});

test("mapContentItems maps current YouTube.js ShortsLockupView rows", () => {
  const items = mapContentItems(
    {
      videos: [{
        type: "ShortsLockupView",
        on_tap_endpoint: { payload: { videoId: "short-1" } },
        overlay_metadata: { primary_text: { text: "Short title" } },
      }],
    },
    "UC_TEST",
  );
  assert.equal(items[0].video_id, "short-1");
  assert.equal(items[0].title, "Short title");
});

test("fetchContentFeed fail-closes when every row is missing video id", async () => {
  const innertube = {
    getChannel: async () => ({
      getVideos: async () => ({
        videos: [{ title: "missing" }],
      }),
    }),
  };
  await assert.rejects(
    () =>
      fetchContentFeed({
        channelId: "UC_TEST",
        kind: "videos",
        innertube,
      }),
    (err) => err.code === "parser_drift",
  );
});

test("fetchContentFeed returns missing_tab without claiming absence", async () => {
  const innertube = {
    getChannel: async () => ({}),
  };
  const result = await fetchContentFeed({
    channelId: "UC_TEST",
    kind: "shorts",
    innertube,
  });
  assert.equal(result.missing_tab, true);
  assert.equal(result.exhausted, false);
  assert.deepEqual(result.items, []);
});

test("fetchContentFeed paginates videos from a stub channel", async () => {
  const innertube = {
    getChannel: async () => ({
      getVideos: async () => ({
        videos: [{ id: "vid-1", title: "One" }],
      }),
    }),
  };
  const result = await fetchContentFeed({
    channelId: "UC_TEST",
    kind: "videos",
    innertube,
  });
  assert.equal(result.items[0].video_id, "vid-1");
  assert.equal(result.exhausted, true);
  assert.equal(result.continuity, "CONTIGUOUS");
});
