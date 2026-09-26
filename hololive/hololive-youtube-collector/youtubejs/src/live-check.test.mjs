import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createServer } from "node:http";
import test from "node:test";

import { createFetchTransport, FetchTransportError } from "./fetch-transport.mjs";
import { fetchChannelLiveCheck, fetchVideoLiveCheck } from "./live-check.mjs";
import { parseRawLiveMetadata } from "./live-metadata.mjs";
import { handleChannelLiveCheckRequest, handleVideoLiveCheckRequest } from "./rpc-boundary.mjs";
import { runWithRequestContext } from "./request-context.mjs";
import {
  validateChannelLiveCheckResponse,
  validateVideoLiveCheckResponse,
} from "./rpc-validation.mjs";

const scenarios = JSON.parse(
  await readFile(new URL("../testdata/live-check-scenarios.json", import.meta.url), "utf8"),
);
const nowMs = Date.parse(scenarios.now);
const clock = () => nowMs;
const videoScenarios = new Map(scenarios.video.map((scenario) => [scenario.name, scenario]));

function playerFor(scenario) {
  return scenario.player_scenario === undefined ? scenario.player : videoScenarios.get(scenario.player_scenario).player;
}

function fakeInnertube(responses) {
  const calls = [];
  return {
    calls,
    actions: {
      async execute(endpoint, payload) {
        calls.push([endpoint, payload]);
        const next = responses[calls.length - 1];
        if (next === undefined) {
          throw new Error(`unexpected upstream call ${calls.length}`);
        }
        if (next instanceof Error) {
          throw next;
        }
        return { success: true, status_code: 200, data: next };
      },
    },
  };
}

const playerPayload = (videoId) => ({ videoId, racyCheckOk: true, contentCheckOk: true, parse: false });


for (const scenario of scenarios.video) {
  test(`video live check ${scenario.name} uses one player request`, async () => {
    const innertube = fakeInnertube([scenario.player]);
    const result = await fetchVideoLiveCheck(innertube, scenario.video_id, clock);
    assert.deepEqual(result, scenario.expected);
    assert.deepEqual(innertube.calls, [["/player", playerPayload(scenario.video_id)]]);
    assert.deepEqual(validateVideoLiveCheckResponse({ protocol_version: 1, ...result }), {
      protocol_version: 1,
      ...scenario.expected,
    });
  });
}

for (const scenario of scenarios.channel) {
  test(`channel live check ${scenario.name} stays within its request budget`, async () => {
    const responses = scenario.expected_calls === 1 ? [scenario.resolve] : [scenario.resolve, playerFor(scenario)];
    const innertube = fakeInnertube(responses);
    const result = await fetchChannelLiveCheck(innertube, scenario.channel_id, clock);
    assert.deepEqual(result, scenario.expected);
    assert.equal(innertube.calls.length, scenario.expected_calls);
    assert.deepEqual(innertube.calls[0], ["/navigation/resolve_url", {
      url: `https://www.youtube.com/channel/${scenario.channel_id}/live`,
      parse: false,
    }]);
    if (scenario.expected_calls === 2) {
      assert.deepEqual(innertube.calls[1], ["/player", playerPayload(scenario.expected.selected_video_id)]);
    }
    assert.deepEqual(validateChannelLiveCheckResponse({ protocol_version: 1, ...result }), {
      protocol_version: 1,
      ...scenario.expected,
    });
  });
}

test("members-only LIVE coexists with a negative upcoming /live resolution for the same channel", async () => {
  for (const counterexample of scenarios.counterexamples) {
    const channelScenario = scenarios.channel.find((scenario) => scenario.name === counterexample.channel_scenario);
    const videoScenario = videoScenarios.get(counterexample.video_scenario);
    const channel = await fetchChannelLiveCheck(
      fakeInnertube([channelScenario.resolve, playerFor(channelScenario)]),
      channelScenario.channel_id,
      clock,
    );
    const video = await fetchVideoLiveCheck(fakeInnertube([videoScenario.player]), videoScenario.video_id, clock);
    assert.equal(channel.outcome, "UPCOMING_VIDEO");
    assert.equal(channel.channel_identity_confirmed, true);
    assert.equal(video.channel_id, channel.channel_id);
    assert.notEqual(video.video_id, channel.selected_video_id);
    assert.equal(video.is_live_now, true);
    assert.equal(video.availability, "MEMBERS_ONLY");
  }
});

test("upstream failures stay typed RPC failures and never become observations", async () => {
  const innertubeFailure = new Error("Request to https://www.youtube.com/youtubei/v1/player failed with status code 404");
  const video = fakeInnertube([innertubeFailure]);
  await assert.rejects(
    () => fetchVideoLiveCheck(video, "failure-fixture", clock),
    (error) => error.code === "collection_failed" && error.failureClass === "TRANSIENT",
  );
  assert.equal(video.calls.length, 1);

  const cooldown = new FetchTransportError("cooldown", "COOLDOWN", "upstream request failed with status code 429");
  const resolveFailure = fakeInnertube([cooldown]);
  await assert.rejects(() => fetchChannelLiveCheck(resolveFailure, "UC_FAILURE", clock), (error) => error === cooldown);
  assert.equal(resolveFailure.calls.length, 1);

  const channelScenario = scenarios.channel.find((scenario) => scenario.name === "upcoming_waiting_measured");
  const playerFailure = fakeInnertube([channelScenario.resolve, new TypeError("fetch failed")]);
  await assert.rejects(
    () => fetchChannelLiveCheck(playerFailure, channelScenario.channel_id, clock),
    (error) => error.code === "collection_failed",
  );
  assert.equal(playerFailure.calls.length, 2);
});

test("non-JSON success bodies are UNKNOWN structure observations", async () => {
  const video = fakeInnertube([new SyntaxError("Unexpected token < in JSON")]);
  assert.deepEqual(await fetchVideoLiveCheck(video, "html-fixture", clock), {
    video_id: "html-fixture",
    identity_confirmed: false,
    availability: "UNKNOWN",
    method: "unknown",
    unknown_reason: "structure_unrecognized",
  });
  const channel = fakeInnertube([new SyntaxError("Unexpected token < in JSON")]);
  assert.deepEqual(await fetchChannelLiveCheck(channel, "UC_HTML", clock), {
    channel_id: "UC_HTML",
    outcome: "UNKNOWN",
    channel_identity_confirmed: false,
    unknown_reason: "structure_unrecognized",
  });
  assert.equal(channel.calls.length, 1);
});

test("single-attempt transport shares the proxy agent and cancellation without retrying", async () => {
  const dispatchers = [];
  let calls = 0;
  const events = [];
  class ProxyAgent {
    async close() {}
    destroy() {}
  }
  const controller = new AbortController();
  const transport = await createFetchTransport({
    proxy: { enabled: true, url: "http://proxy.test:8080" },
    currentSignal: () => controller.signal,
    retryDelayMs: 0,
    observeRetry: (event) => events.push(event),
    loadUndici: async () => ({
      ProxyAgent,
      fetch: async (_url, init) => {
        calls += 1;
        dispatchers.push(init.dispatcher);
        return new Response("unavailable", { status: 503 });
      },
    }),
  });
  try {
    const player = () => ["https://www.youtube.com/youtubei/v1/player", { method: "POST", body: "{}" }];
    await assert.rejects(transport.singleAttemptFetch(...player()), (error) => error.code === "collection_failed");
    assert.equal(calls, 1);
    assert.deepEqual(events, []);

    await assert.rejects(transport.fetch(...player()), (error) => error.code === "collection_failed");
    assert.equal(calls, 3, "legacy endpoints keep their single retry");
    assert.equal(events.length, 1);
    assert.equal(new Set(dispatchers).size, 1);

    controller.abort();
    await assert.rejects(transport.singleAttemptFetch(...player()), (error) => error.code === "collection_canceled");
    assert.equal(calls, 3);
  } finally {
    await transport.close();
  }
});

test("live check RPC boundary enforces exact subjects and flat results", async () => {
  const video = scenarios.video.find((scenario) => scenario.name === "ended_premiere_measured");
  const ok = await handleVideoLiveCheckRequest(
    JSON.stringify({ protocol_version: 1, video_id: video.video_id, max_success_response_bytes: 4096 }),
    () => video.expected,
    1 << 20,
  );
  assert.equal(ok.status, 200);
  assert.deepEqual(ok.body, { protocol_version: 1, ...video.expected });

  const mismatch = await handleVideoLiveCheckRequest(
    JSON.stringify({ protocol_version: 1, video_id: "requested-video", max_success_response_bytes: 4096 }),
    () => video.expected,
    1 << 20,
  );
  assert.equal(mismatch.status, 422);
  assert.equal(mismatch.body.error.code, "parser_drift");

  for (const body of [
    { protocol_version: 1, video_id: " padded ", max_success_response_bytes: 4096 },
    { protocol_version: 1, video_id: "x", max_success_response_bytes: 4096, max_pages: 1 },
    { protocol_version: 2, video_id: "x", max_success_response_bytes: 4096 },
  ]) {
    const rejected = await handleVideoLiveCheckRequest(JSON.stringify(body), () => {
      throw new Error("fetcher must not run");
    }, 1 << 20);
    assert.equal(rejected.status, 400);
    assert.equal(rejected.body.error.code, "invalid_request");
  }

  const channel = scenarios.channel.find((scenario) => scenario.name === "channel_page_measured");
  const leaked = await handleChannelLiveCheckRequest(
    JSON.stringify({ protocol_version: 1, channel_id: channel.channel_id, max_success_response_bytes: 4096 }),
    () => ({ ...channel.expected, page_count: 1 }),
    1 << 20,
  );
  assert.equal(leaked.status, 422);
  assert.equal(leaked.body.error.code, "parser_drift");
});

test("live check response validation rejects results the Go contract would reject", () => {
  const base = { protocol_version: 1, video_id: "v", channel_id: "UC", identity_confirmed: true };
  for (const invalid of [
    { ...base, availability: "PUBLIC", method: "player_public" },
    { ...base, is_private: true, availability: "PUBLIC", method: "player_public" },
    { ...base, is_private: false, availability: "PUBLIC_UNAVAILABLE", method: "player_private" },
    { ...base, availability: "MEMBERS_ONLY", method: "player_public" },
    { ...base, availability: "UNKNOWN", method: "unknown" },
    { ...base, availability: "UNKNOWN", method: "unknown", unknown_reason: "request_failed" },
    { ...base, availability: "UNKNOWN", method: "unknown", unknown_reason: "identity_mismatch" },
    { protocol_version: 1, video_id: "v", identity_confirmed: false, is_live: true, availability: "UNKNOWN", method: "unknown", unknown_reason: "identity_missing" },
    { ...base, is_live: true, ended_at: "2026-09-20T10:00:00.000Z", availability: "MEMBERS_ONLY", method: "player_members_only" },
    { ...base, is_live_now: false, ended_at: "2026-09-20T10:00:00Z", availability: "MEMBERS_ONLY", method: "player_members_only" },
  ]) {
    assert.throws(() => validateVideoLiveCheckResponse(invalid), (error) => error.code === "parser_drift");
  }
  for (const invalid of [
    { protocol_version: 1, channel_id: "UC", outcome: "CHANNEL_PAGE", selected_video_id: "v", channel_identity_confirmed: true },
    { protocol_version: 1, channel_id: "UC", outcome: "UPCOMING_VIDEO", channel_identity_confirmed: true },
    { protocol_version: 1, channel_id: "UC", outcome: "LIVE_VIDEO", selected_video_id: "v", channel_identity_confirmed: false },
    { protocol_version: 1, channel_id: "UC", outcome: "UNKNOWN", channel_identity_confirmed: true, unknown_reason: "identity_missing" },
    { protocol_version: 1, channel_id: "UC", outcome: "UNKNOWN", channel_identity_confirmed: false, unknown_reason: "availability_unclassified" },
  ]) {
    assert.throws(() => validateChannelLiveCheckResponse(invalid), (error) => error.code === "parser_drift");
  }
});

test("canceled live checks remain RPC cancellations", async () => {
  const controller = new AbortController();
  controller.abort();
  const canceled = new DOMException("aborted", "AbortError");
  const innertube = fakeInnertube([canceled]);
  const result = await runWithRequestContext({ requestId: "cancel", signal: controller.signal }, () =>
    handleVideoLiveCheckRequest(
      JSON.stringify({ protocol_version: 1, video_id: "cancel-fixture", max_success_response_bytes: 4096 }),
      (options) => fetchVideoLiveCheck(innertube, options.videoId, clock),
      1 << 20,
    ));
  assert.equal(result.status, 408);
  assert.equal(result.body.error.code, "collection_canceled");
});

test("legacy raw metadata additionally exposes channel, isLiveNow and end facts without changing admission", () => {
  const premiere = videoScenarios.get("ended_premiere_measured");
  assert.deepEqual(parseRawLiveMetadata(premiere.player, premiere.video_id), {
    videoId: "H9Sutl-r6YY",
    channelId: "UC_SANITIZED_PREMIERE_CHANNEL",
    isLiveNow: false,
    isLiveContent: false,
    startTimestamp: "2026-09-20T10:00:00.000Z",
    endTimestamp: "2026-09-20T10:05:00.000Z",
  });
  const malformedEnd = structuredClone(premiere.player);
  malformedEnd.microformat.playerMicroformatRenderer.liveBroadcastDetails.endTimestamp = "yesterday";
  malformedEnd.microformat.playerMicroformatRenderer.liveBroadcastDetails.isLiveNow = "no";
  malformedEnd.videoDetails.channelId = 42;
  assert.deepEqual(parseRawLiveMetadata(malformedEnd, premiere.video_id), {
    videoId: "H9Sutl-r6YY",
    isLiveContent: false,
    startTimestamp: "2026-09-20T10:00:00.000Z",
  });
});

test("single-attempt checks do not follow redirects into an extra upstream request", async (t) => {
  const paths = [];
  const server = createServer((request, response) => {
    paths.push(request.url);
    response.writeHead(request.url === "/player" ? 307 : 204, { location: "/followed" });
    response.end();
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  t.after(() => new Promise((resolve) => {
    server.closeAllConnections();
    server.close(resolve);
  }));
  const transport = await createFetchTransport({ proxy: { enabled: false }, currentSignal: () => undefined });
  t.after(() => transport.close());
  const url = `http://127.0.0.1:${server.address().port}/player`;
  await assert.rejects(transport.singleAttemptFetch(url, { method: "POST", body: "{}" }), TypeError);
  assert.deepEqual(paths, ["/player"]);
  paths.length = 0;
  const legacy = await transport.fetch(url, { method: "POST", body: "{}" });
  assert.equal(legacy.status, 204);
  assert.deepEqual(paths, ["/player", "/followed"]);
});

test("private and membership facts cannot become trusted live evidence", async () => {
  const scenario = scenarios.video.find((item) => item.category === "members_live");
  const raw = structuredClone(scenario.player);
  raw.videoDetails.isPrivate = true;
  const result = await fetchVideoLiveCheck(fakeInnertube([raw]), scenario.video_id, clock);
  assert.equal(result.availability, "UNKNOWN");
  assert.equal(result.unknown_reason, "contradictory_fields");
});

test("unrecognized playability structures invalidate otherwise valid live facts", async () => {
  const scenario = scenarios.video.find((item) => item.category === "public_live");
  for (const status of ["", "UNRECOGNIZED_STATUS"]) {
    const raw = structuredClone(scenario.player);
    raw.playabilityStatus.status = status;
    const result = await fetchVideoLiveCheck(fakeInnertube([raw]), scenario.video_id, clock);
    assert.equal(result.availability, "UNKNOWN");
    assert.equal(result.unknown_reason, "structure_unrecognized");
  }
});
