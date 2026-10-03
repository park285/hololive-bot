import assert from "node:assert/strict";
import test from "node:test";
import { Utils } from "youtubei.js";
import { YT, YTNodes } from "youtubei.js";

import { fetchChannelFeed } from "./fetch-channel.mjs";
import { fetchCommunityFeed } from "./fetch-community.mjs";
import { fetchContentFeed } from "./fetch-content.mjs";
import { fetchLiveMetadata } from "./live-metadata.mjs";
import { handleChannelRequest, handleCommunityRequest, handleContentRequest } from "./rpc-boundary.mjs";
import { rpcErrorResultFor } from "./rpc-validation.mjs";
import { runWithRequestContext } from "./request-context.mjs";
import { channelFixture } from "./test-fixtures/channel.mjs";

const tabCases = [
  { kind: "videos", tab: "videos", getter: "has_videos", method: "getVideos", fetch: fetchContentFeed, handle: handleContentRequest },
  { kind: "shorts", tab: "shorts", getter: "has_shorts", method: "getShorts", fetch: fetchContentFeed, handle: handleContentRequest },
  { tab: "posts", getter: "has_community", method: "getCommunity", fetch: fetchCommunityFeed, handle: handleCommunityRequest },
  { kind: "live", tab: "streams", getter: "has_live_streams", method: "getLiveStreams", fetch: fetchChannelFeed, handle: handleChannelRequest },
];

function collect(scenario, getChannel) {
  return scenario.handle(JSON.stringify({
    protocol_version: 1, channel_id: "UC_TEST", max_success_response_bytes: 1048576,
    ...(scenario.kind === undefined ? {} : { kind: scenario.kind }),
  }), (options) => scenario.fetch({ ...options, innertube: { getChannel } }));
}

for (const scenario of tabCases) {
  test(`${scenario.tab} uses raw absence evidence regardless of library getter overrides`, async () => {
    for (const useGetter of [false, true]) {
      const channel = await channelFixture(useGetter
        ? { [scenario.method]: async () => assert.fail("confirmed missing tab must not be requested") }
        : { [scenario.getter]: undefined }, ["featured"]);
      const result = await collect(scenario, async () => channel);
      assert.equal(result.status, 200);
      assert.equal(result.body.missing_tab, true);
      assert.equal(result.body.continuity, "NOT_APPLICABLE");
    }
  });

  test(`${scenario.tab} keeps unrelated library errors and null pages as failures`, async () => {
    for (const failure of [
      new Utils.InnertubeError("channel does not exist"),
      new Utils.InnertubeError(`Tab "different" not found`),
      new TypeError("malformed library response"),
    ]) {
      const result = await collect(scenario, async () => channelFixture({ [scenario.method]: async () => { throw failure; } }));
      assert.equal(result.status, 502);
      assert.equal(result.body.error.code, "collection_failed");
      assert.equal(result.body.missing_tab, undefined);
    }
    const result = await collect(scenario, async () => channelFixture({ [scenario.method]: async () => null }));
    assert.notEqual(result.status, 200);
    assert.equal(result.body.missing_tab, undefined);
  });

  test(`${scenario.tab} rejects malformed real library tab endpoints before absence checks`, async () => {
    const channel = new YT.Channel({}, { data: { contents: { twoColumnBrowseResultsRenderer: { tabs: [{
      tabRenderer: {
        title: "Malformed tab",
        endpoint: { commandMetadata: { webCommandMetadata: { url: 17 } }, browseEndpoint: { browseId: "UC_TEST" } },
      },
    }] } } } });
    assert.throws(() => channel[scenario.getter], TypeError);
    const result = await collect(scenario, async () => channel);
    assert.equal(result.status, 422);
    assert.equal(result.body.error.code, "parser_drift");
  });

  test(`${scenario.tab} preserves explicit helper defects and parent cancellation`, async () => {
    const defect = Object.assign(new TypeError("helper invariant failed"), { code: "helper_internal_invariant" });
    const result = await collect(scenario, async () => { throw defect; });
    assert.equal(result.status, 500);
    assert.equal(result.body.error.code, "helper_internal_invariant");

    const controller = new AbortController();
    const canceled = await runWithRequestContext({ requestId: scenario.tab, signal: controller.signal }, () =>
      collect(scenario, async () => channelFixture({ [scenario.method]: async () => {
        controller.abort();
        throw new Utils.InnertubeError(`Tab "${scenario.tab}" not found`);
      } })),
    );
    assert.equal(canceled.status, 408);
    assert.equal(canceled.body.error.code, "collection_canceled");
  });
}

test("missing channel and HTTP 404 do not become a missing community tab", async () => {
  for (const failure of [
    new Utils.InnertubeError("channel does not exist"),
    new Utils.InnertubeError("Request to https://www.youtube.com/youtubei/v1/browse failed with status code 404", "{}"),
  ]) {
    const result = await collect(tabCases[2], async () => { throw failure; });
    assert.equal(result.status, 502);
    assert.equal(result.body.error.code, "collection_failed");
  }
});

test("metadata library exceptions preserve upstream provenance", async () => {
  await assert.rejects(fetchLiveMetadata({ actions: { execute: async () => {
    throw new TypeError("unexpected upstream player shape");
  } } }, "video-id"), (error) => {
    assert.equal(rpcErrorResultFor(error).body.error.code, "collection_failed");
    assert.ok(error.cause instanceof TypeError);
    return true;
  });
});

test("malformed real library text stays a data-contract failure instead of a helper TypeError", async () => {
  const row = new YTNodes.Video({ videoId: "video-1", title: { simpleText: 17 } });
  const result = await collect(tabCases[0], async () => channelFixture({ getVideos: async () => channelFixture({ videos: [row] }) }));
  assert.equal(result.status, 422);
  assert.equal(result.body.error.code, "parser_drift");
  assert.equal(result.body.error.class, "DATA_CONTRACT");
});
