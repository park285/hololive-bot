// @ts-check
import { createInnertube, fetchCommunityFeed } from "./fetch-community.mjs";
import { fetchContentFeed } from "./fetch-content.mjs";
import { fetchChannelFeed } from "./fetch-channel.mjs";
import { createLiveCheckInnertube, fetchChannelLiveCheck, fetchVideoLiveCheck } from "./live-check.mjs";

/** @typedef {import("./contracts.d.ts").FetcherSet} FetcherSet */
/** @typedef {import("./upstream-feeds.d.ts").InnertubeFetch} InnertubeFetch */

/**
 * @param {{
 *   fetchImpl?: InnertubeFetch,
 *   singleAttemptFetchImpl?: InnertubeFetch,
 *   createInnertubeImpl?: typeof createInnertube,
 *   createLiveCheckInnertubeImpl?: typeof createLiveCheckInnertube,
 * }} [options]
 * @returns {FetcherSet}
 */
export function createRealFetchers(options = {}) {
  const fetchImpl = options.fetchImpl;
  const singleAttemptFetchImpl = options.singleAttemptFetchImpl;
  const initInnertube = options.createInnertubeImpl ?? createInnertube;
  const initLiveCheckInnertube = options.createLiveCheckInnertubeImpl ?? createLiveCheckInnertube;
  /** @type {Promise<unknown> | undefined} */
  let innertubePromise;
  /** @type {Promise<unknown> | undefined} */
  let liveCheckInnertubePromise;

  async function innertubeClient() {
    if (innertubePromise == null) {
      innertubePromise = initInnertube({ fetchImpl }).catch((err) => {
        innertubePromise = undefined;
        throw err;
      });
    }
    return innertubePromise;
  }

  // 라이브 확인은 기존 수집 Innertube의 재시도 transport를 공유하지 않고, 단일 시도 transport에 묶인 별도 인스턴스를 씁니다.
  async function liveCheckInnertubeClient() {
    if (liveCheckInnertubePromise == null) {
      liveCheckInnertubePromise = initLiveCheckInnertube({ fetchImpl: singleAttemptFetchImpl }).catch((err) => {
        liveCheckInnertubePromise = undefined;
        throw err;
      });
    }
    return liveCheckInnertubePromise;
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
    async fetchChannelLiveCheck(fetchOptions) {
      return fetchChannelLiveCheck(await liveCheckInnertubeClient(), fetchOptions.channelId);
    },
    async fetchVideoLiveCheck(fetchOptions) {
      return fetchVideoLiveCheck(await liveCheckInnertubeClient(), fetchOptions.videoId);
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
  // stub helper에는 upstream 응답이 없으므로 확인 결과를 음성이나 가용성으로 만들지 않습니다.
  fetchChannelLiveCheck(fetchOptions) {
    return {
      channel_id: fetchOptions.channelId,
      outcome: "UNKNOWN",
      channel_identity_confirmed: false,
      unknown_reason: "structure_unrecognized",
    };
  },
  fetchVideoLiveCheck(fetchOptions) {
    return {
      video_id: fetchOptions.videoId,
      identity_confirmed: false,
      availability: "UNKNOWN",
      method: "unknown",
      unknown_reason: "structure_unrecognized",
    };
  },
  async close() {},
};
