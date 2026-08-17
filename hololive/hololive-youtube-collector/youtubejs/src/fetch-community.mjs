import { mapPost } from "./map-posts.mjs";
import {
  assertResponseBudget,
  paginate,
  paginationEnvelopeReserve,
  paginationResult,
} from "./pagination.mjs";

const responseReserveBytes = paginationEnvelopeReserve({ protocol_version: 1, posts: [] });

export function listBackstagePosts(feed, postType) {
  if (feed == null) {
    return [];
  }
  if (Array.isArray(feed.posts)) {
    return feed.posts;
  }
  if (typeof feed.memo?.getType === "function") {
    const typed = feed.memo.getType(postType) || [];
    return [...typed];
  }
  const error = new Error("community page shape is not recognized");
  error.code = "parser_drift";
  throw error;
}

export function isMissingCommunity(err) {
  const message = String(err?.message || err);
  return /tab not found/i.test(message) || /channel does not exist/i.test(message) || err?.status === 404;
}

export async function fetchCommunityPosts(options = {}) {
  const result = await fetchCommunityFeed(options);
  return result.posts;
}

/** @param {YouTubeJSFetchOptions} [options] */
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
    throw new Error("channel id is required");
  }
  if (innertube == null || typeof innertube.getChannel !== "function") {
    throw new Error("innertube client is required");
  }
  assertResponseBudget(maxSuccessResponseBytes, responseReserveBytes);
  let channel;
  try {
    channel = await innertube.getChannel(id);
  } catch (err) {
    if (isMissingCommunity(err)) {
      return emptyCommunityPage();
    }
    throw err;
  }
  if (channel?.has_community === false) {
    return emptyCommunityPage();
  }
  let feed;
  try {
    feed = await channel.getCommunity();
  } catch (err) {
    if (isMissingCommunity(err)) {
      return emptyCommunityPage();
    }
    throw err;
  }
  const paged = await paginate({
    firstPage: feed,
    getContinuation: async (current) => {
      if (typeof current.getContinuation !== "function") {
        const err = new Error("community continuation is missing");
        err.code = "parser_drift";
        throw err;
      }
      return current.getContinuation();
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
  return Innertube.create({
    retrieve_player: false,
    generate_session_locally: true,
    enable_session_cache: false,
    fetch: fetchImpl,
  });
}
