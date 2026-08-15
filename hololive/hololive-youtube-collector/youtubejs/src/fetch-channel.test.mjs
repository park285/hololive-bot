import assert from "node:assert/strict";
import test from "node:test";

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
          content_image: { overlays: [{ badges: [{ text: "Upcoming" }] }] },
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
