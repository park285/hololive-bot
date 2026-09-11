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
const restrictedFixture = JSON.parse(
  await readFile(new URL("../testdata/player-members-only.json", import.meta.url), "utf8"),
);

test("one restricted schedule preserves normal upcoming and live rows through the RPC", async (t) => {
  const logs = [];
  t.mock.method(process.stderr, "write", (line) => { logs.push(JSON.parse(line)); return true; });
  const calls = [];
  const innertube = stubChannel({ videos: [
    { id: "upcoming-a", is_upcoming: true },
    { id: "restricted-fixture", is_upcoming: true },
    { id: "upcoming-b", is_upcoming: true },
    { id: "already-live", is_live: true },
  ] }, async (_, { videoId }) => {
    calls.push(videoId);
    return videoId === "restricted-fixture"
      ? { success: true, status_code: 200, data: restrictedFixture }
      : rawPlayerResponse(videoId);
  });
  const result = await handleChannelRequest(
    JSON.stringify({ protocol_version: 1, kind: "live", channel_id: "UC_TEST", max_success_response_bytes: 1048576 }),
    (options) => fetchChannelFeed({ ...options, innertube }),
  );
  assert.equal(result.status, 200);
  assert.deepEqual(result.body.live_sessions.map((item) => [item.video_id, item.status]), [
    ["upcoming-a", "UPCOMING"], ["upcoming-b", "UPCOMING"], ["already-live", "LIVE"],
  ]);
  assert.deepEqual(result.body.unavailable_live_sessions, [
    { video_id: "restricted-fixture", channel_id: "UC_TEST", reason: "access_restricted" },
  ]);
  assert.deepEqual(calls, ["upcoming-a", "restricted-fixture", "upcoming-b"]);
  assert.equal(logs.length, 1);
  assert.equal(logs[0].event, "youtubejs_live_schedule_unavailable");
  assert.deepEqual(logs[0].video_ids, ["restricted-fixture"]);
});

test("a restricted-only feed preserves its unresolved identity and is rechecked next poll", async (t) => {
  t.mock.method(process.stderr, "write", () => true);
  let calls = 0;
  const innertube = stubChannel({ videos: [{ id: "restricted-fixture", is_upcoming: true }] }, async () => {
    calls++;
    return calls === 1 ? { success: true, status_code: 200, data: restrictedFixture } : rawPlayerResponse("restricted-fixture");
  });
  const first = await fetchChannelFeed({ kind: "live", channelId: "UC_TEST", innertube });
  assert.deepEqual(first.live_sessions, []);
  assert.equal(first.unavailable_live_sessions[0].video_id, "restricted-fixture");
  const second = await fetchChannelFeed({ kind: "live", channelId: "UC_TEST", innertube });
  assert.equal(second.live_sessions[0].scheduled_at, "2026-09-01T11:00:00.000Z");
  assert.equal(second.unavailable_live_sessions, undefined);
  assert.equal(calls, 2);
});

test("restricted filtering never hides a conflicting duplicate or foreign channel row", async (t) => {
  t.mock.method(process.stderr, "write", () => true);
  const conflicts = [
    { id: "restricted-fixture", is_live: true },
    { id: "restricted-fixture", is_upcoming: true, scheduled: "2026-09-11T03:00:00Z" },
    { id: "restricted-fixture", is_upcoming: true, author: { id: "UC_OTHER" } },
  ];
  for (const conflict of conflicts) {
    const innertube = stubChannel({ videos: [
      conflict,
      { id: "restricted-fixture", is_upcoming: true },
    ] }, async () => ({ success: true, status_code: 200, data: restrictedFixture }));
    await assert.rejects(
      () => fetchChannelFeed({ kind: "live", channelId: "UC_TEST", innertube }),
      (error) => error.code === "parser_drift",
    );
  }
});

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
    () => fetchChannelFeed({ kind: "live", channelId: "UC_TEST", innertube }),
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
  const result = await fetchChannelFeed({ kind: "live", channelId: "UC_TEST", innertube });
  assert.deepEqual(result.live_sessions, []);
  assert.equal(result.missing_tab, true);
  assert.deepEqual(result.stats, {});
  assert.deepEqual(result.profile, {});
});

test("fetchChannelFeed signals an unsupported live streams tab without claiming live absence", async () => {
  const innertube = {
    getChannel: async () => ({
      getAbout: async () => ({ subscriber_count: 7, handle: "@unsupported" }),
    }),
  };
  const result = await fetchChannelFeed({ kind: "live", channelId: "UC_TEST", innertube });
  assert.deepEqual(result.live_sessions, []);
  assert.equal(result.missing_tab, true);
  assert.deepEqual(result.stats, {});
  assert.deepEqual(result.profile, {});
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
    () => fetchChannelFeed({ kind: "live", channelId: "UC_TEST", innertube }),
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
    () => fetchChannelFeed({ kind: "live", channelId: "UC_TEST", innertube }),
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

test("metadata collection returns channel fields without requesting streams or player", async () => {
  const innertube = {
    getChannel: async () => ({
      getAbout: async () => ({ subscriber_count: 12, handle: "@test", description: "hi" }),
      getLiveStreams: async () => { throw new Error("metadata requested streams"); },
    }),
    actions: {
      execute: async () => { throw new Error("metadata requested player"); },
    },
  };
  const result = await fetchChannelFeed({ kind: "metadata", channelId: "UC_TEST", innertube });
  assert.deepEqual(result.live_sessions, []);
  assert.equal(result.stats.subscriber_count, 12);
  assert.equal(result.profile.handle, "@test");
  assert.equal(result.exhausted, true);
});

test("live collection does not depend on the about endpoint", async () => {
  const innertube = {
    getChannel: async () => ({
      getAbout: async () => { throw new Error("live requested about"); },
      getLiveStreams: async () => ({ videos: [{ id: "live-1", is_live: true }] }),
    }),
  };
  const result = await fetchChannelFeed({ kind: "live", channelId: "UC_TEST", innertube });
  assert.equal(result.live_sessions[0].status, "LIVE");
});

test("fetchChannelFeed enriches a LockupView fixture without parsing display text", async () => {
  const calls = [];
  const innertube = stubChannel(lockupFixture, async (endpoint, payload) => {
    calls.push([endpoint, payload.videoId]);
    return { success: true, status_code: 200, data: playerFixture };
  });

  const result = await fetchChannelFeed({ kind: "live", channelId: "UC_TEST", innertube });

  assert.deepEqual(calls, [["/player", "upcoming-fixture"]]);
  assert.equal(result.live_sessions[0].scheduled_at, "2026-09-01T11:00:00.000Z");
});

test("fetchChannelFeed recovers a schedule from the raw player offline slate", async () => {
  const response = rawPlayerResponse("offline-slate", { startTimestamp: undefined });
  response.data.playabilityStatus = {
    liveStreamability: {
      liveStreamabilityRenderer: {
        videoId: "offline-slate",
        offlineSlate: {
          liveStreamOfflineSlateRenderer: { scheduledStartTime: "1788260400" },
        },
      },
    },
  };
  const result = await fetchChannelFeed({
    kind: "live",
    channelId: "UC_TEST",
    innertube: stubChannel({ videos: [{ id: "offline-slate", is_upcoming: true }] }, async () => response),
  });

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
    kind: "live",
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
    kind: "live",
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
    kind: "live",
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
    kind: "live",
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
    kind: "live",
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
    kind: "live",
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
    JSON.stringify({ protocol_version: 1, kind: "live", channel_id: "UC_TEST", max_success_response_bytes: 1048576 }),
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
      JSON.stringify({ protocol_version: 1, kind: "live", channel_id: "UC_TEST", max_success_response_bytes: 1048576 }),
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
