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
