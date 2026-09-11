import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { fetchLiveMetadata, parseRawLiveMetadata } from "./live-metadata.mjs";

const playerFixture = JSON.parse(
  await readFile(new URL("../testdata/player-upcoming.json", import.meta.url), "utf8"),
);
const restrictedFixture = JSON.parse(
  await readFile(new URL("../testdata/player-members-only.json", import.meta.url), "utf8"),
);

test("paid-access metadata preserves a known missing schedule without parsing localized reasons", () => {
  const raw = structuredClone(restrictedFixture);
  raw.playabilityStatus.reason = "채널 회원 전용 콘텐츠";
  assert.deepEqual(parseRawLiveMetadata(raw, "restricted-fixture"), {
    videoId: "restricted-fixture", isUpcoming: true, isLiveContent: true,
    scheduleUnavailableReason: "access_restricted",
  });
});

test("restricted streams with a machine-readable schedule still use that schedule", () => {
  const raw = structuredClone(restrictedFixture);
  raw.microformat = structuredClone(playerFixture.microformat);
  const metadata = parseRawLiveMetadata(raw, "restricted-fixture");
  assert.equal(metadata.startTimestamp, "2026-09-01T11:00:00.000Z");
  assert.equal(metadata.scheduleUnavailableReason, undefined);
});

test("unknown restrictions and non-live content do not acquire the bounded exception", () => {
  const cases = [
    (raw) => { delete raw.playabilityStatus.errorScreen; },
    (raw) => { raw.playabilityStatus.status = "LOGIN_REQUIRED"; },
    (raw) => { delete raw.videoDetails.isUpcoming; },
    (raw) => { raw.videoDetails.isUpcoming = false; },
    (raw) => { raw.videoDetails.isLiveContent = false; },
  ];
  for (const change of cases) {
    const raw = structuredClone(restrictedFixture);
    change(raw);
    assert.equal(parseRawLiveMetadata(raw, "restricted-fixture").scheduleUnavailableReason, undefined);
  }
});

test("paid-access metadata never masks malformed identity, state, renderer, or schedule", () => {
  const cases = [
    (raw) => { raw.videoDetails.videoId = "wrong-video"; },
    (raw) => { raw.videoDetails.isUpcoming = "true"; },
    (raw) => { raw.videoDetails.isLive = true; },
    (raw) => { raw.playabilityStatus.errorScreen.playerLegacyDesktopYpcOfferRenderer = null; },
    (raw) => { raw.microformat = { playerMicroformatRenderer: { liveBroadcastDetails: { startTimestamp: "tomorrow" } } }; },
  ];
  for (const change of cases) {
    const raw = structuredClone(restrictedFixture);
    change(raw);
    assert.throws(() => parseRawLiveMetadata(raw, "restricted-fixture"), (err) => err.code === "parser_drift");
  }
});

test("parseRawLiveMetadata reads the exact matching raw player schedule", () => {
  assert.deepEqual(parseRawLiveMetadata(playerFixture, "upcoming-fixture"), {
    videoId: "upcoming-fixture",
    isLive: false,
    isUpcoming: true,
    isLiveContent: true,
    startTimestamp: "2026-09-01T11:00:00.000Z",
  });
});

test("parseRawLiveMetadata recovers an upcoming schedule from the offline slate", () => {
  const raw = {
    videoDetails: {
      videoId: "upcoming-offline-slate",
      isUpcoming: true,
      isLiveContent: true,
    },
    playabilityStatus: {
      liveStreamability: {
        liveStreamabilityRenderer: {
          videoId: "upcoming-offline-slate",
          offlineSlate: {
            liveStreamOfflineSlateRenderer: { scheduledStartTime: "1788260400" },
          },
        },
      },
    },
  };

  assert.deepEqual(parseRawLiveMetadata(raw, "upcoming-offline-slate"), {
    videoId: "upcoming-offline-slate",
    isUpcoming: true,
    isLiveContent: true,
    startTimestamp: "2026-09-01T11:00:00.000Z",
  });
});

test("parseRawLiveMetadata rejects malformed or contradictory offline slate schedules", () => {
  const raw = {
    videoDetails: { videoId: "upcoming-offline-slate", isUpcoming: true },
    playabilityStatus: {
      liveStreamability: {
        liveStreamabilityRenderer: {
          videoId: "upcoming-offline-slate",
          offlineSlate: {
            liveStreamOfflineSlateRenderer: { scheduledStartTime: "tomorrow" },
          },
        },
      },
    },
  };

  assert.throws(
    () => parseRawLiveMetadata(raw, "upcoming-offline-slate"),
    (error) => error.code === "parser_drift",
  );
  raw.playabilityStatus.liveStreamability.liveStreamabilityRenderer.offlineSlate
    .liveStreamOfflineSlateRenderer.scheduledStartTime = "1788260400000";
  assert.throws(
    () => parseRawLiveMetadata(raw, "upcoming-offline-slate"),
    (error) => error.code === "parser_drift",
  );
  raw.playabilityStatus.liveStreamability.liveStreamabilityRenderer.videoId = "different-video";
  raw.playabilityStatus.liveStreamability.liveStreamabilityRenderer.offlineSlate
    .liveStreamOfflineSlateRenderer.scheduledStartTime = "1788260400";
  assert.throws(
    () => parseRawLiveMetadata(raw, "upcoming-offline-slate"),
    (error) => error.code === "parser_drift",
  );
});

test("parseRawLiveMetadata rejects disagreeing machine-readable schedules", () => {
  const raw = structuredClone(playerFixture);
  raw.playabilityStatus = {
    liveStreamability: {
      liveStreamabilityRenderer: {
        videoId: "upcoming-fixture",
        offlineSlate: {
          liveStreamOfflineSlateRenderer: { scheduledStartTime: "1788260460" },
        },
      },
    },
  };

  assert.throws(
    () => parseRawLiveMetadata(raw, "upcoming-fixture"),
    (error) => error.code === "parser_drift",
  );
});

test("parseRawLiveMetadata rejects identity, boolean, and timestamp drift", () => {
  assert.throws(
    () => parseRawLiveMetadata(playerFixture, "different-video"),
    (error) => error.code === "parser_drift",
  );
  assert.throws(
    () => parseRawLiveMetadata({
      ...playerFixture,
      videoDetails: { ...playerFixture.videoDetails, isUpcoming: "true" },
    }, "upcoming-fixture"),
    (error) => error.code === "parser_drift",
  );
  assert.throws(
    () => parseRawLiveMetadata({
      ...playerFixture,
      microformat: {
        playerMicroformatRenderer: {
          liveBroadcastDetails: { startTimestamp: "September 1, 2026 8:00 PM" },
        },
      },
    }, "upcoming-fixture"),
    (error) => error.code === "parser_drift",
  );
});

test("fetchLiveMetadata rejects a malformed Actions response", async () => {
  await assert.rejects(
    () => fetchLiveMetadata({
      actions: { execute: async () => ({ status_code: 200, data: playerFixture }) },
    }, "upcoming-fixture"),
    (error) => error.code === "parser_drift",
  );
});

test("fetchLiveMetadata uses one raw player request with the required payload", async () => {
  const calls = [];
  const innertube = {
    actions: {
      execute: async (...args) => {
        calls.push(args);
        return { success: true, status_code: 200, data: playerFixture };
      },
    },
  };

  await fetchLiveMetadata(innertube, "upcoming-fixture");

  assert.deepEqual(calls, [["/player", {
    videoId: "upcoming-fixture",
    racyCheckOk: true,
    contentCheckOk: true,
    parse: false,
  }]]);
});

test("fetchLiveMetadata preserves status-bearing and cancellation failures", async () => {
  let forbidden;
  await assert.rejects(
    () => fetchLiveMetadata({
      actions: { execute: async () => ({ success: false, status_code: 403, data: {} }) },
    }, "upcoming-fixture"),
    (error) => {
      forbidden = error;
      return true;
    },
  );
  assert.equal(forbidden.status, 403);

  const canceled = new DOMException("aborted", "AbortError");
  await assert.rejects(
    () => fetchLiveMetadata({
      actions: { execute: async () => { throw canceled; } },
    }, "upcoming-fixture"),
    (error) => error === canceled,
  );
});
