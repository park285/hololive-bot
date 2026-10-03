import { channelFixture } from "./test-fixtures/channel.mjs";
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
    getChannel: async () => channelFixture({
      getVideos: async () => channelFixture({
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

test("PAG-011 fetchContentFeed recognizes a complete raw list without the shorts tab", async () => {
  const innertube = {
    getChannel: async () => channelFixture({ has_shorts: undefined }, ["featured"]),
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
    getChannel: async () => channelFixture({
      getVideos: async () => channelFixture({
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

test("videos content RPC makes no per-row player calls and drops lockup-derived times", async () => {
  const innertube = {
    getChannel: async () => channelFixture({
      getVideos: async () => channelFixture({
        videos: [{
          type: "LockupView",
          content_type: "VIDEO",
          content_id: "premiere-1",
          metadata: { title: "Premiere" },
          content_image: { overlays: [{ badges: [{ text: "Upcoming" }] }] },
          published: "3 hours ago",
          scheduled: "2026-08-24T14:30:00Z",
        }],
      }),
    }),
    actions: { execute: async () => assert.fail("content RPC must not hide per-video player requests") },
  };

  const result = await fetchContentFeed({ channelId: "UC_TEST", kind: "videos", innertube });

  assert.deepEqual(result.items, [{ video_id: "premiere-1", channel_id: "UC_TEST", title: "Premiere", is_upcoming: true }]);
});

test("content result budget stops before validating unselected rows", async () => {
  const innertube = {
    getChannel: async () => channelFixture({ getVideos: async () => channelFixture({ videos: [
      { id: "first", is_upcoming: true },
      { id: "second", is_upcoming: true },
      { title: "unselected malformed row" },
    ] }) }),
    actions: { execute: async () => assert.fail("content RPC must not request player metadata") },
  };
  const result = await fetchContentFeed({ channelId: "UC_TEST", kind: "videos", maxResults: 1, innertube });
  assert.equal(result.termination_reason, "max_results");
  assert.deepEqual(result.items.map((item) => item.video_id), ["first"]);
});

test("shorts result budget skips unselected normalization without metadata requests", async () => {
  const innertube = {
    getChannel: async () => channelFixture({ getShorts: async () => channelFixture({ videos: [
      { id: "first", is_upcoming: true }, { title: "unselected malformed" },
    ] }) }),
    actions: { execute: async () => assert.fail("shorts must not request premiere metadata") },
  };
  const result = await fetchContentFeed({ channelId: "UC_TEST", kind: "shorts", maxResults: 1, innertube });
  assert.equal(result.items[0].video_id, "first");
  assert.equal(result.termination_reason, "max_results");
});

test("mapContentItems preserves fail-closed validation for sparse rows", () => {
  assert.throws(() => mapContentItems({ videos: new Array(1) }, "UC_TEST"),
    (error) => error.code === "parser_drift");
});
