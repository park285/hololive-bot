import assert from "node:assert/strict";
import test from "node:test";

import {
  fetchCommunityFeed,
  fetchCommunityPosts,
  isMissingCommunity,
  listBackstagePosts,
} from "./fetch-community.mjs";

test("listBackstagePosts reads memo.getType", () => {
  const posts = listBackstagePosts(
    {
      memo: {
        getType: () => [{ id: "post-1" }],
      },
    },
    class BackstagePost {},
  );
  assert.equal(posts[0].id, "post-1");
});

test("isMissingCommunity recognizes tab-not-found", () => {
  assert.equal(isMissingCommunity(new Error("Community tab not found")), true);
  assert.equal(isMissingCommunity(new Error("rate limited")), false);
});

test("fetchCommunityFeed maps stub Innertube posts without live YouTube", async () => {
  const innertube = {
    getChannel: async (channelId) => ({
      has_community: true,
      getCommunity: async () => ({
        memo: {
          getType: () => [
            {
              id: `${channelId}-post`,
              author: { id: channelId, name: "Author", thumbnails: [] },
              content: "hello",
              published: "now",
            },
          ],
        },
      }),
    }),
  };
  const result = await fetchCommunityFeed({
    channelId: "UC_TEST",
    maxResults: 10,
    innertube,
  });
  assert.equal(result.posts.length, 1);
  assert.equal(result.posts[0].postId, "UC_TEST-post");
  assert.equal(result.page_count, 1);
  assert.equal(result.exhausted, true);
  assert.equal(result.continuity, "CONTIGUOUS");
});

test("PAG-011 fetchCommunityFeed keeps missing tab as a capability signal", async () => {
  const innertube = {
    getChannel: async () => ({
      has_community: false,
      getCommunity: async () => {
        throw new Error("should not run");
      },
    }),
  };
  const result = await fetchCommunityFeed({ channelId: "UC_NONE", innertube });
  assert.equal(result.missing_tab, true);
  assert.equal(result.continuity, "NOT_APPLICABLE");
  assert.equal(result.termination_reason, "exhausted");
  assert.deepEqual(result.posts, []);
});

test("fetchCommunityPosts returns empty when the posts tab is missing", async () => {
  const innertube = {
    getChannel: async () => ({
      has_community: false,
      getCommunity: async () => {
        throw new Error("should not run");
      },
    }),
  };
  const posts = await fetchCommunityPosts({ channelId: "UC_NONE", innertube });
  assert.deepEqual(posts, []);
});

test("fetchCommunityFeed fail-closes on Innertube errors", async () => {
  const innertube = {
    getChannel: async () => {
      throw new Error("innertube unavailable");
    },
  };
  await assert.rejects(
    () => fetchCommunityFeed({ channelId: "UC_FAIL", innertube }),
    /innertube unavailable/,
  );
});

test("fetchCommunityFeed fail-closes when a community post id is missing", async () => {
  const innertube = {
    getChannel: async () => ({
      has_community: true,
      getCommunity: async () => ({
        posts: [{ author: { id: "UC_TEST", name: "Author" }, content: "missing id" }],
      }),
    }),
  };
  await assert.rejects(
    () => fetchCommunityFeed({ channelId: "UC_TEST", innertube }),
    (error) => error.code === "parser_drift",
  );
});

test("fetchCommunityFeed preserves continuation metadata across pages", async () => {
  const innertube = {
    getChannel: async () => ({
      has_community: true,
      getCommunity: async () => ({
        continuation: "page-2",
        posts: [{ id: "post-1", author: { id: "UC_TEST", name: "Author" }, content: "one" }],
        getContinuation: async () => ({
          posts: [{ id: "post-2", author: { id: "UC_TEST", name: "Author" }, content: "two" }],
        }),
      }),
    }),
  };
  const result = await fetchCommunityFeed({
    channelId: "UC_TEST",
    maxPages: 2,
    innertube,
  });
  assert.equal(result.posts.length, 2);
  assert.equal(result.page_count, 2);
  assert.equal(result.exhausted, true);
  assert.equal(result.cursor_start, "page-2");
});

test("youtubei.js preserves attachment runs without length", async (t) => {
  const { Misc } = await import("youtubei.js");
  const warnings = [];
  const originalWarn = console.warn;
  console.warn = (...args) => warnings.push(args);
  t.after(() => {
    console.warn = originalWarn;
  });

  const parsed = Misc.Text.fromAttributed({
    content: "Lui ch. and Laplus ch.",
    attachmentRuns: [{ startIndex: 8, element: { type: {}, properties: {} }, alignment: "ALIGNMENT_VERTICAL_CENTER" }],
  });

  assert.deepEqual(warnings, []);
  assert.equal(parsed.text, "Lui ch. and Laplus ch.");
  assert.equal(parsed.runs.length, 1);
  assert.equal(parsed.runs[0].attachment.startIndex, 8);
  assert.equal(parsed.runs[0].attachment.length, 0);
});
