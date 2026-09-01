import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { Utils } from "youtubei.js";

import { fetchChannelFeed, mapLiveSessions, mapPhoto, mapProfile, mapStats } from "./fetch-channel.mjs";
import { handleChannelRequest } from "./rpc-boundary.mjs";
import { runWithRequestContext } from "./request-context.mjs";

const lockupFixture = JSON.parse(
  await readFile(new URL("../testdata/lockup-upcoming.json", import.meta.url), "utf8"),
);
const playerFixture = JSON.parse(
  await readFile(new URL("../testdata/player-upcoming.json", import.meta.url), "utf8"),
);

test("mapLiveSessions fail-closes on unknown statuses", () => {
  assert.throws(
    () =>
      mapLiveSessions(
        {
          videos: [
            { id: "live-1", is_live: true },
            { id: "mystery", status: "unknown" },
          ],
        },
        "UC_TEST",
      ),
    (err) => err.code === "parser_drift",
  );
});

test("mapLiveSessions maps current YouTube.js LockupView rows", () => {
  const sessions = mapLiveSessions(
    {
      videos: [
        {
          type: "LockupView",
          content_type: "VIDEO",
          content_id: "upcoming-1",
          metadata: { title: "Upcoming title" },
          content_image: {
            sources: [{ url: "https://i.ytimg.com/vi/upcoming-1/maxresdefault.jpg", width: 1280, height: 720 }],
            overlays: [{ badges: [{ text: "Upcoming" }] }],
          },
        },
        { type: "LockupView", content_type: "VIDEO", content_id: "ended-1", content_image: { overlays: [] } },
      ],
    },
    "UC_TEST",
  );
  assert.deepEqual(sessions.map((item) => [item.video_id, item.status]), [
    ["upcoming-1", "UPCOMING"],
    ["ended-1", "ENDED"],
  ]);
  assert.equal(sessions[0].title, "Upcoming title");
  assert.equal(sessions[0].thumbnail_url, "https://i.ytimg.com/vi/upcoming-1/maxresdefault.jpg");
});

test("fetchChannelFeed fail-closes when live rows lack status", async () => {
  const innertube = {
    getChannel: async () => ({
      getAbout: async () => ({}),
      getLiveStreams: async () => ({ videos: [{ id: "mystery", status: "unknown" }] }),
    }),
  };
  await assert.rejects(
    () => fetchChannelFeed({ channelId: "UC_TEST", innertube }),
    (err) => err.code === "parser_drift",
  );
});

test("fetchChannelFeed signals a typed missing streams tab without claiming live absence", async () => {
  const innertube = {
    getChannel: async () => ({
      getAbout: async () => ({ subscriber_count: 12, handle: "@test" }),
      getLiveStreams: async () => {
        throw new Utils.InnertubeError('Tab "streams" not found');
      },
    }),
  };
  const result = await fetchChannelFeed({ channelId: "UC_TEST", innertube });
  assert.deepEqual(result.live_sessions, []);
  assert.equal(result.missing_tab, true);
  assert.equal(result.stats.subscriber_count, 12);
  assert.equal(result.profile.handle, "@test");
});

test("fetchChannelFeed signals an unsupported live streams tab without claiming live absence", async () => {
  const innertube = {
    getChannel: async () => ({
      getAbout: async () => ({ subscriber_count: 7, handle: "@unsupported" }),
    }),
  };
  const result = await fetchChannelFeed({ channelId: "UC_TEST", innertube });
  assert.deepEqual(result.live_sessions, []);
  assert.equal(result.missing_tab, true);
  assert.equal(result.stats.subscriber_count, 7);
  assert.equal(result.profile.handle, "@unsupported");
});

test("fetchChannelFeed propagates a typed error with a different message", async () => {
  const expected = new Utils.InnertubeError("streams request failed");
  const innertube = {
    getChannel: async () => ({
      getAbout: async () => ({}),
      getLiveStreams: async () => {
        throw expected;
      },
    }),
  };
  await assert.rejects(
    () => fetchChannelFeed({ channelId: "UC_TEST", innertube }),
    (err) => err === expected,
  );
});

test("fetchChannelFeed propagates an untyped missing streams error", async () => {
  const expected = new Error('Tab "streams" not found');
  const innertube = {
    getChannel: async () => ({
      getAbout: async () => ({}),
      getLiveStreams: async () => {
        throw expected;
      },
    }),
  };
  await assert.rejects(
    () => fetchChannelFeed({ channelId: "UC_TEST", innertube }),
    (err) => err === expected,
  );
});

test("mapStats preserves missing counts as null", () => {
  assert.equal(mapStats({}, {}).subscriber_count, null);
});

test("mapProfile keeps empty fields as null", () => {
  assert.equal(mapProfile({}, {}).handle, null);
});

test("mapPhoto maps avatar and banner variants", () => {
  const variants = mapPhoto(
    { author: { thumbnails: [{ url: "https://img.test/a.jpg", width: 88, height: 88 }] } },
    { banner: [{ url: "https://img.test/b.jpg", width: 100, height: 20 }] },
  );
  assert.equal(variants[0].kind, "avatar");
  assert.equal(variants[1].kind, "banner");
});

test("fetchChannelFeed returns typed channel fields from a stub", async () => {
  const innertube = {
    getChannel: async () => ({
      getAbout: async () => ({ subscriber_count: 12, handle: "@test", description: "hi" }),
      getLiveStreams: async () => ({ videos: [{ id: "vid-1", is_upcoming: true }] }),
    }),
    actions: {
      execute: async () => rawPlayerResponse("vid-1"),
    },
  };
  const result = await fetchChannelFeed({ channelId: "UC_TEST", innertube });
  assert.equal(result.live_sessions[0].status, "UPCOMING");
  assert.equal(result.stats.subscriber_count, 12);
  assert.equal(result.profile.handle, "@test");
  assert.equal(result.exhausted, true);
});

test("fetchChannelFeed enriches a LockupView fixture without parsing display text", async () => {
  const calls = [];
  const innertube = stubChannel(lockupFixture, async (endpoint, payload) => {
    calls.push([endpoint, payload.videoId]);
    return { success: true, status_code: 200, data: playerFixture };
  });

  const result = await fetchChannelFeed({ channelId: "UC_TEST", innertube });

  assert.deepEqual(calls, [["/player", "upcoming-fixture"]]);
  assert.equal(result.live_sessions[0].scheduled_at, "2026-09-01T11:00:00.000Z");
});

test("fetchChannelFeed preserves list schedules and skips non-upcoming rows", async () => {
  let calls = 0;
  const feed = {
    videos: [
      { id: "scheduled", is_upcoming: true, scheduled: new Date("2026-09-01T20:00:00+09:00") },
      { id: "live", is_live: true },
      { id: "ended", status: "ENDED" },
      { id: "canceled", status: "CANCELLED" },
    ],
  };
  const result = await fetchChannelFeed({
    channelId: "UC_TEST",
    innertube: stubChannel(feed, async () => {
      calls += 1;
      return rawPlayerResponse("unused");
    }),
  });

  assert.equal(calls, 0);
  assert.equal(result.live_sessions[0].scheduled_at, "2026-09-01T11:00:00.000Z");
});

test("fetchChannelFeed never accepts localized list text as a schedule", async () => {
  let calls = 0;
  const result = await fetchChannelFeed({
    channelId: "UC_TEST",
    innertube: stubChannel({
      videos: [{ id: "localized", is_upcoming: true, scheduled: "September 1, 2026 8:00 PM" }],
    }, async (_endpoint, payload) => {
      calls += 1;
      return rawPlayerResponse(payload.videoId);
    }),
  });

  assert.equal(calls, 1);
  assert.equal(result.live_sessions[0].scheduled_at, "2026-09-01T11:00:00.000Z");
});

test("fetchChannelFeed deduplicates missing schedules and preserves request order", async () => {
  const requested = [];
  const feed = {
    videos: [
      { id: "first", is_upcoming: true },
      { id: "first", is_upcoming: true },
      { id: "second", is_upcoming: true },
    ],
  };
  const result = await fetchChannelFeed({
    channelId: "UC_TEST",
    innertube: stubChannel(feed, async (_endpoint, payload) => {
      requested.push(payload.videoId);
      return rawPlayerResponse(payload.videoId);
    }),
  });

  assert.deepEqual(requested, ["first", "second"]);
  assert.deepEqual(result.live_sessions.map((session) => session.scheduled_at), [
    "2026-09-01T11:00:00.000Z",
    "2026-09-01T11:00:00.000Z",
    "2026-09-01T11:00:00.000Z",
  ]);
});

test("fetchChannelFeed permits exactly 32 metadata lookups", async () => {
  const requested = [];
  const feed = { videos: Array.from({ length: 32 }, (_, index) => ({ id: `video-${index}`, is_upcoming: true })) };
  await fetchChannelFeed({
    channelId: "UC_TEST",
    innertube: stubChannel(feed, async (_endpoint, payload) => {
      requested.push(payload.videoId);
      return rawPlayerResponse(payload.videoId);
    }),
  });
  assert.equal(requested.length, 32);
});

test("fetchChannelFeed rejects 33 candidates before a metadata request", async () => {
  let calls = 0;
  const feed = { videos: Array.from({ length: 33 }, (_, index) => ({ id: `video-${index}`, is_upcoming: true })) };
  await assert.rejects(
    () => fetchChannelFeed({
      channelId: "UC_TEST",
      innertube: stubChannel(feed, async () => {
        calls += 1;
        return rawPlayerResponse("unused");
      }),
    }),
    (error) => error.code === "parser_drift",
  );
  assert.equal(calls, 0);
});

test("fetchChannelFeed keeps a list-to-player LIVE transition catch-up eligible", async () => {
  const response = rawPlayerResponse("transitioned", {
    isLive: true,
    isUpcoming: false,
    startTimestamp: "2026-09-01T11:01:08Z",
  });
  const result = await fetchChannelFeed({
    channelId: "UC_TEST",
    innertube: stubChannel({ videos: [{ id: "transitioned", is_upcoming: true }] }, async () => response),
  });

  assert.equal(result.live_sessions[0].status, "LIVE");
  assert.equal(result.live_sessions[0].scheduled_at, undefined);
  assert.equal(result.live_sessions[0].started_at, "2026-09-01T11:01:08.000Z");
});

test("incomplete UPCOMING becomes a typed 422 RPC failure", async () => {
  const innertube = stubChannel(
    { videos: [{ id: "unresolved", is_upcoming: true }] },
    async () => rawPlayerResponse("unresolved", { startTimestamp: undefined }),
  );
  const result = await handleChannelRequest(
    JSON.stringify({ protocol_version: 1, channel_id: "UC_TEST", max_success_response_bytes: 1048576 }),
    (options) => fetchChannelFeed({ ...options, innertube }),
  );

  assert.equal(result.status, 422);
  assert.equal(result.body.error.code, "parser_drift");
});

test("fetchChannelFeed cancellation remains a typed canceled RPC failure", async () => {
  const controller = new AbortController();
  controller.abort(new DOMException("aborted", "AbortError"));
  const innertube = stubChannel(
    { videos: [{ id: "canceled", is_upcoming: true }] },
    async () => { throw controller.signal.reason; },
  );
  const result = await runWithRequestContext(
    { requestId: "channel-canceled", signal: controller.signal },
    () => handleChannelRequest(
      JSON.stringify({ protocol_version: 1, channel_id: "UC_TEST", max_success_response_bytes: 1048576 }),
      (options) => fetchChannelFeed({ ...options, innertube }),
    ),
  );

  assert.equal(result.status, 408);
  assert.equal(result.body.error.code, "collection_canceled");
});

function stubChannel(feed, execute) {
  return {
    getChannel: async () => ({
      getAbout: async () => ({}),
      getLiveStreams: async () => feed,
    }),
    actions: { execute },
  };
}

function rawPlayerResponse(videoId, options = {}) {
  const startTimestamp = Object.hasOwn(options, "startTimestamp")
    ? options.startTimestamp
    : "2026-09-01T11:00:00Z";
  return {
    success: true,
    status_code: 200,
    data: {
      videoDetails: {
        videoId,
        isLive: options.isLive ?? false,
        isLiveContent: true,
        isUpcoming: options.isUpcoming ?? true,
      },
      microformat: {
        playerMicroformatRenderer: {
          liveBroadcastDetails: {
            ...(startTimestamp == null ? {} : { startTimestamp }),
          },
        },
      },
    },
  };
}
