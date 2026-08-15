import { mapPost } from "./map-posts.mjs";
import { paginate } from "./pagination.mjs";

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
  return [];
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
  maxAggregateBytes,
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
        err.code = "pagination_gap";
        throw err;
      }
      return current.getContinuation();
    },
    mapPage: (current) => {
      const mapped = [];
      for (const post of listBackstagePosts(current, postType)) {
        const item = mapPost(post);
        if (item != null) {
          mapped.push(item);
        }
      }
      return mapped;
    },
    maxPages,
    maxResults,
    maxAggregateBytes,
  });
  return {
    posts: paged.items,
    page_count: paged.page_count,
    cursor_start: paged.cursor_start,
    cursor_end: paged.cursor_end,
    exhausted: paged.exhausted,
    continuity: paged.continuity,
  };
}

export function emptyCommunityPage() {
  return {
    posts: [],
    page_count: 0,
    exhausted: false,
    continuity: "GAP_UNRESOLVED",
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
