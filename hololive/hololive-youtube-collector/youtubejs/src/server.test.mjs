import assert from "node:assert/strict";
import test from "node:test";

import {
  handleChannelRequest,
  handleCommunityRequest,
  handleContentRequest,
  handleViewerRequest,
  setProxyUrl,
} from "./server.mjs";

test("handleCommunityRequest requires channel_id", async () => {
  const result = await handleCommunityRequest("{}", async () => ({}));
  assert.equal(result.status, 400);
  assert.match(result.body.error, /channel_id/);
});

test("handleCommunityRequest returns pagination metadata from the injected fetcher", async () => {
  const result = await handleCommunityRequest(
    JSON.stringify({ channel_id: "UC_TEST", max_results: 3, max_pages: 2, proxy_url: "http://proxy.test:8080" }),
    async ({ channelId, maxResults, maxPages }) => {
      assert.equal(channelId, "UC_TEST");
      assert.equal(maxResults, 3);
      assert.equal(maxPages, 2);
      return {
        posts: [{
          postId: "post-1",
          upstreamPostId: "post-1",
          authorId: "UC_AUTHOR",
          authorName: "Author",
          authorPhoto: [],
          contentText: "hello",
          publishedText: "1 hour ago",
          likeCount: 1,
          commentCount: 0,
          images: [],
        }],
        page_count: 1,
        exhausted: true,
        continuity: "CONTIGUOUS",
      };
    },
  );
  assert.equal(result.status, 200);
  assert.equal(result.body.posts[0].postId, "post-1");
  assert.equal(result.body.page_count, 1);
  assert.equal(result.body.exhausted, true);
  assert.equal(result.body.continuity, "CONTIGUOUS");
});

test("handleCommunityRequest fail-closes when the fetcher throws", async () => {
  const result = await handleCommunityRequest(
    JSON.stringify({ channel_id: "UC_FAIL" }),
    async () => {
      throw new Error("innertube down");
    },
  );
  assert.equal(result.status, 500);
  assert.match(result.body.error, /innertube down/);
  assert.equal(result.body.error_code, "collection_failed");
});

test("handleCommunityRequest rejects invalid JSON", async () => {
  const result = await handleCommunityRequest("{", async () => ({}));
  assert.equal(result.status, 400);
});

test("handleCommunityRequest rejects coercible non-string ids", async () => {
  const result = await handleCommunityRequest(JSON.stringify({ channel_id: { toString: "UC_FAKE" } }), async () => ({}));
  assert.equal(result.status, 400);
  assert.match(result.body.error, /channel_id/);
});

test("handleContentRequest requires kind", async () => {
  const result = await handleContentRequest(JSON.stringify({ channel_id: "UC_TEST" }), async () => ({}));
  assert.equal(result.status, 400);
  assert.match(result.body.error, /kind/);
});

test("handleChannelRequest returns the injected channel payload", async () => {
  const result = await handleChannelRequest(
    JSON.stringify({ channel_id: "UC_TEST" }),
    async ({ channelId }) => ({
      live_sessions: [{ video_id: "vid-1", channel_id: channelId, status: "LIVE" }],
      stats: {},
      profile: {},
      photo: [],
      page_count: 1,
      exhausted: true,
      continuity: "CONTIGUOUS",
    }),
  );
  assert.equal(result.status, 200);
  assert.equal(result.body.live_sessions[0].video_id, "vid-1");
});

test("handleChannelRequest rejects an unvalidated fetcher response", async () => {
  const result = await handleChannelRequest(
    JSON.stringify({ channel_id: "UC_TEST" }),
    async () => ({ live_sessions: "not-an-array" }),
  );
  assert.equal(result.status, 500);
  assert.equal(result.body.error_code, "parser_drift");
});

test("handleChannelRequest strips fields outside the validated response contract", async () => {
  const result = await handleChannelRequest(
    JSON.stringify({ channel_id: "UC_TEST" }),
    async () => ({
      live_sessions: [], stats: {}, profile: {}, photo: [],
      page_count: 0, exhausted: true, continuity: "CONTIGUOUS",
      unchecked: "must-not-cross-the-rpc-boundary",
    }),
  );
  assert.equal(result.status, 200);
  assert.equal("unchecked" in result.body, false);
  assert.equal(result.body.page_count, 0);
});

test("handleViewerRequest requires video_id", async () => {
  const result = await handleViewerRequest("{}", async () => ({}));
  assert.equal(result.status, 400);
  assert.match(result.body.error, /video_id/);
});

test("setProxyUrl stores the helper proxy", () => {
  setProxyUrl(" http://proxy.test ");
  setProxyUrl("");
});
