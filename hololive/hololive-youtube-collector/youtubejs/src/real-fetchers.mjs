// @ts-check
import { createInnertube, fetchCommunityFeed } from "./fetch-community.mjs";
import { fetchContentFeed } from "./fetch-content.mjs";
import { fetchChannelFeed } from "./fetch-channel.mjs";
import { fetchViewerFeed } from "./fetch-viewer.mjs";

/** @typedef {import("./contracts.d.ts").FetcherSet} FetcherSet */
/** @typedef {import("./upstream-feeds.d.ts").InnertubeFetch} InnertubeFetch */

/**
 * @param {{
 *   fetchImpl?: InnertubeFetch,
 *   createInnertubeImpl?: typeof createInnertube,
 * }} [options]
 * @returns {FetcherSet}
 */
export function createRealFetchers(options = {}) {
  const fetchImpl = options.fetchImpl;
  const initInnertube = options.createInnertubeImpl ?? createInnertube;
  /** @type {Promise<unknown> | undefined} */
  let innertubePromise;

  async function innertubeClient() {
    if (innertubePromise == null) {
      innertubePromise = initInnertube({ fetchImpl }).catch((err) => {
        innertubePromise = undefined;
        throw err;
      });
    }
    return innertubePromise;
  }

  return {
    async fetchCommunity(fetchOptions) {
      const innertube = await innertubeClient();
      const youtubejs = await import("youtubei.js");
      return fetchCommunityFeed({
        ...fetchOptions,
        innertube,
        postType: youtubejs.YTNodes.BackstagePost,
      });
    },
    async fetchContent(fetchOptions) {
      return fetchContentFeed({
        ...fetchOptions,
        innertube: await innertubeClient(),
      });
    },
    async fetchChannel(fetchOptions) {
      return fetchChannelFeed({
        ...fetchOptions,
        innertube: await innertubeClient(),
      });
    },
    async fetchViewer(fetchOptions) {
      return fetchViewerFeed({
        ...fetchOptions,
        innertube: await innertubeClient(),
      });
    },
    async close() {},
  };
}

/** @satisfies {FetcherSet} */
export const stubFetchers = {
  fetchCommunity() {
    return {
      posts: [],
      page_count: 1,
      exhausted: true,
      continuity: "CONTIGUOUS",
      termination_reason: "exhausted",
    };
  },
  fetchContent() {
    return {
      items: [],
      page_count: 1,
      exhausted: true,
      continuity: "CONTIGUOUS",
      termination_reason: "exhausted",
    };
  },
  fetchChannel() {
    return {
      live_sessions: [],
      stats: {},
      profile: {},
      photo: [],
      page_count: 1,
      exhausted: true,
      continuity: "NOT_APPLICABLE",
      termination_reason: "exhausted",
    };
  },
  fetchViewer(fetchOptions) {
    return {
      video_id: fetchOptions.videoId,
      viewer_count: null,
      availability: "UNAVAILABLE",
      page_count: 1,
      exhausted: true,
      continuity: "NOT_APPLICABLE",
      termination_reason: "exhausted",
    };
  },
  async close() {},
};
