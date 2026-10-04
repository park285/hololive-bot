import { createCollectionClient } from "./collection-client.mjs";
import { FetchTransportError } from "./fetch-transport.mjs";
import { readUpstream, runUpstream } from "./upstream-errors.mjs";
import { fetchChannelTab } from "./channel-tabs.mjs";
import { mapPost } from "./map-posts.mjs";
import {
  assertResponseBudget,
  paginate,
  paginationEnvelopeReserve,
  paginationResult,
} from "./pagination.mjs";

const responseReserveBytes = paginationEnvelopeReserve({ protocol_version: 1, posts: [] });

export function listBackstagePosts(feed, postType) {
  const posts = readUpstream(() => feed?.posts);
  if (Array.isArray(posts)) {
    return posts;
  }
  const memo = readUpstream(() => feed?.memo);
  if (typeof memo?.getType === "function") {
    const typed = readUpstream(() => memo.getType(postType)) || [];
    return [...typed];
  }
  const error = new Error("community page shape is not recognized");
  error.code = "parser_drift";
  throw error;
}

export async function fetchCommunityPosts(options = {}) {
  const result = await fetchCommunityFeed(options);
  return result.posts;
}

/**
 * @param {YouTubeJSFetchOptions} [options]
 * @returns {Promise<Omit<import("./contracts.d.ts").CommunityResult, "protocol_version">>}
 */
export async function fetchCommunityFeed({
  channelId,
  maxResults,
  maxPages,
  maxSuccessResponseBytes = Number.MAX_SAFE_INTEGER,
  innertube,
  postType,
} = {}) {
  const id = String(channelId ?? "").trim();
  if (id === "") {
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "channel id is required");
  }
  if (innertube == null || typeof innertube.getChannel !== "function") {
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "innertube client is required");
  }
  assertResponseBudget(maxSuccessResponseBytes, responseReserveBytes);
  const channel = await runUpstream(() => innertube.getChannel(id));
  if (typeof channel.getCommunity !== "function") {
    throw new FetchTransportError("helper_internal_invariant", "INTERNAL", "community tab loader is unavailable");
  }
  const tab = await fetchChannelTab(channel, "posts", () => channel.getCommunity());
  if (tab.missing === true) return emptyCommunityPage();
  const paged = await paginate({
    firstPage: tab.feed,
    getContinuation: async (current) => {
      if (typeof current.getContinuation !== "function") {
        const err = new Error("community continuation is missing");
        err.code = "parser_drift";
        throw err;
      }
      return runUpstream(() => current.getContinuation());
    },
    mapPage: (current) => {
      const mapped = [];
      for (const post of listBackstagePosts(current, postType)) {
        const item = mapPost(post);
        if (item == null) {
          const error = new Error("community post id is missing");
          error.code = "parser_drift";
          throw error;
        }
        mapped.push(item);
      }
      return { recognized_shape: true, items: mapped };
    },
    maxPages,
    maxResults,
    maxSuccessResponseBytes,
    reservedEnvelopeBytes: responseReserveBytes,
    buildResult: (posts, pagination) => ({ posts, ...pagination }),
  });
  return paged;
}

/** @returns {Omit<import("./contracts.d.ts").CommunityResult, "protocol_version">} */
export function emptyCommunityPage() {
  return {
    posts: [],
    ...paginationResult({
      pageCount: 1,
      reason: "exhausted",
      continuity: "NOT_APPLICABLE",
    }),
    missing_tab: true,
  };
}

/** @param {YouTubeJSFetchOptions} [options] */
export async function createInnertube({ fetchImpl } = {}) {
  const { Innertube } = await import("youtubei.js");
  return createCollectionClient(await Innertube.create({
    retrieve_player: false,
    generate_session_locally: true,
    enable_session_cache: false,
    fetch: fetchImpl,
  }));
}
