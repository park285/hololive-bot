import assert from "node:assert/strict";
import test from "node:test";
import { Utils } from "youtubei.js";

import { fetchChannelFeed, mapLiveSessions, mapPhoto, mapProfile, mapStats } from "./fetch-channel.mjs";

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
  };
  const result = await fetchChannelFeed({ channelId: "UC_TEST", innertube });
  assert.equal(result.live_sessions[0].status, "UPCOMING");
  assert.equal(result.stats.subscriber_count, 12);
  assert.equal(result.profile.handle, "@test");
  assert.equal(result.exhausted, true);
});
