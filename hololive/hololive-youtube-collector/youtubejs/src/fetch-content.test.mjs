import assert from "node:assert/strict";
import test from "node:test";

import { fetchContentFeed, mapContentItems } from "./fetch-content.mjs";

test("PAG-001 first page transport failure is fatal", async () => {
  const expected = new Error("connection reset");
  expected.code = "ECONNRESET";
  const innertube = {
    getChannel: async () => {
      throw expected;
    },
  };
  await assert.rejects(
    () => fetchContentFeed({
      channelId: "UC_TEST",
      kind: "videos",
      innertube,
    }),
    (error) => error === expected,
  );
});

test("PAG-008 rejects an undersized response budget before fetching", async () => {
  let calls = 0;
  const innertube = {
    getChannel: async () => {
      calls += 1;
      return {};
    },
  };
  await assert.rejects(
    () => fetchContentFeed({
      channelId: "UC_TEST",
      kind: "videos",
      maxSuccessResponseBytes: 100,
      innertube,
    }),
    (error) => error.code === "response_too_large",
  );
  assert.equal(calls, 0);
});

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

test("PAG-011 fetchContentFeed keeps missing_tab without claiming absence", async () => {
  const innertube = {
    getChannel: async () => ({}),
  };
  const result = await fetchContentFeed({
    channelId: "UC_TEST",
    kind: "shorts",
    innertube,
  });
  assert.equal(result.missing_tab, true);
  assert.equal(result.continuity, "NOT_APPLICABLE");
  assert.equal(result.termination_reason, "exhausted");
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

test("fetchContentFeed enriches an upcoming premiere with its start timestamp", async () => {
  const startTimestamp = "2026-08-24T14:30:00.000Z";
  const innertube = {
    getChannel: async () => ({
      getVideos: async () => ({
        videos: [{
          type: "LockupView",
          content_type: "VIDEO",
          content_id: "premiere-1",
          metadata: { title: "Premiere" },
          content_image: { overlays: [{ badges: [{ text: "Upcoming" }] }] },
        }],
      }),
    }),
    getInfo: async (videoId) => {
      assert.equal(videoId, "premiere-1");
      return {
        basic_info: {
          is_upcoming: true,
          is_live_content: false,
          start_timestamp: startTimestamp,
        },
      };
    },
  };

  const result = await fetchContentFeed({
    channelId: "UC_TEST",
    kind: "videos",
    innertube,
  });

  assert.equal(result.items[0].scheduled_for, startTimestamp);
  assert.equal(result.items[0].is_premiere, true);
});

test("fetchContentFeed does not classify upcoming live content as a premiere", async () => {
  const innertube = {
    getChannel: async () => ({
      getVideos: async () => ({
        videos: [{
          id: "live-1",
          title: "Live",
          content_image: { overlays: [{ badges: [{ text: "Upcoming" }] }] },
        }],
      }),
    }),
    getInfo: async () => ({
      basic_info: {
        is_upcoming: true,
        is_live_content: true,
        start_timestamp: "2026-08-24T14:30:00.000Z",
      },
    }),
  };

  const result = await fetchContentFeed({
    channelId: "UC_TEST",
    kind: "videos",
    innertube,
  });

  assert.equal(result.items[0].scheduled_for, undefined);
  assert.equal(result.items[0].is_premiere, undefined);
});

test("fetchContentFeed keeps a confirmed premiere typed without a start timestamp", async () => {
  const innertube = {
    getChannel: async () => ({
      getVideos: async () => ({
        videos: [{
          id: "premiere-1",
          title: "Premiere",
          content_image: { overlays: [{ badges: [{ text: "Upcoming" }] }] },
        }],
      }),
    }),
    getInfo: async () => ({ basic_info: { is_upcoming: true, is_live_content: false } }),
  };

  const result = await fetchContentFeed({ channelId: "UC_TEST", kind: "videos", innertube });

  assert.equal(result.items[0].is_premiere, true);
  assert.equal(result.items[0].scheduled_for, undefined);
});
