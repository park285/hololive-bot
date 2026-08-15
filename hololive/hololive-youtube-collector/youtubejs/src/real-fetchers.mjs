// @ts-check
import { createInnertube, fetchCommunityFeed } from "./fetch-community.mjs";
import { fetchContentFeed } from "./fetch-content.mjs";
import { fetchChannelFeed } from "./fetch-channel.mjs";
import { fetchViewerFeed } from "./fetch-viewer.mjs";

/** @typedef {import("./contracts.d.ts").FetcherSet} FetcherSet */
/** @typedef {import("./upstream-feeds.d.ts").InnertubeFetch} InnertubeFetch */

let proxyUrl = "";
/** @type {Promise<unknown> | undefined} */
let innertubePromise;

/** @type {import("./contracts.d.ts").ProxyConfigurator} */
export function setProxyUrl(url) {
  proxyUrl = String(url ?? "").trim();
}

/** @type {InnertubeFetch} */
async function proxiedFetch(input, init = {}) {
  if (proxyUrl === "") {
    return globalThis.fetch(
      /** @type {Parameters<typeof fetch>[0]} */ (input),
      /** @type {RequestInit | undefined} */ (init),
    );
  }
  const undici = await import("undici");
  return undici.fetch(/** @type {string} */ (String(input)), {
    dispatcher: new undici.ProxyAgent(proxyUrl),
  });
}

async function innertubeClient() {
  if (innertubePromise == null) {
    innertubePromise = createInnertube({ fetchImpl: proxiedFetch });
  }
  return innertubePromise;
}

/** @satisfies {FetcherSet} */
export const realFetchers = {
  async fetchCommunity(options) {
    const innertube = await innertubeClient();
    const youtubejs = await import("youtubei.js");
    return fetchCommunityFeed({
      ...options,
      innertube,
      postType: youtubejs.YTNodes.BackstagePost,
    });
  },
  async fetchContent(options) {
    return fetchContentFeed({
      ...options,
      innertube: await innertubeClient(),
    });
  },
  async fetchChannel(options) {
    return fetchChannelFeed({
      ...options,
      innertube: await innertubeClient(),
    });
  },
  async fetchViewer(options) {
    return fetchViewerFeed({
      ...options,
      innertube: await innertubeClient(),
    });
  },
};

/** @satisfies {FetcherSet} */
export const stubFetchers = {
  fetchCommunity() {
    return {
      posts: [],
      page_count: 1,
      exhausted: true,
      continuity: "CONTIGUOUS",
    };
  },
  fetchContent() {
    return {
      items: [],
      page_count: 1,
      exhausted: true,
      continuity: "CONTIGUOUS",
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
      continuity: "CONTIGUOUS",
    };
  },
  fetchViewer(options) {
    return {
      video_id: options.videoId,
      viewer_count: null,
      availability: "UNAVAILABLE",
      page_count: 1,
      exhausted: true,
      continuity: "CONTIGUOUS",
    };
  },
};
