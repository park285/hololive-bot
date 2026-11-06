// @ts-check
import { createInnertube, fetchCommunityFeed } from "./fetch-community.mjs";
import { fetchContentFeed } from "./fetch-content.mjs";
import { fetchChannelFeed } from "./fetch-channel.mjs";
import { createLiveCheckInnertube, fetchChannelLiveCheck, fetchVideoLiveCheck } from "./live-check.mjs";
import { ProofController } from "./proof-controller.mjs";
import { ProofError } from "./proof-broker.mjs";

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
  /** @type {Promise<import("youtubei.js").Innertube> | undefined} */
  let liveCheckInnertubePromise;
  /** @type {ProofController | undefined} */
  let proof;

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
      liveCheckInnertubePromise = initLiveCheckInnertube({ fetchImpl: singleAttemptFetchImpl }).then((client) => {
        const userAgent = client.session.user_agent;
        if (typeof userAgent !== "string" || userAgent !== client.session.context.client.userAgent) {
          throw new ProofError("proof_ua_mismatch");
        }
        if (typeof singleAttemptFetchImpl !== "function") throw new ProofError("proof_configuration");
        proof = new ProofController({ fetchImpl: singleAttemptFetchImpl, userAgent });
        return client;
      }).catch((err) => {
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
      return fetchChannelLiveCheck(await liveCheckInnertubeClient(), fetchOptions.channelId, Date.now, proof);
    },
    async fetchVideoLiveCheck(fetchOptions) {
      return fetchVideoLiveCheck(await liveCheckInnertubeClient(), fetchOptions.videoId, Date.now, proof);
    },
    // 공유 수집 Innertube는 bootstrap에서 요청 context 밖에 만듭니다. 첫 RPC의 취소·제한 시간에 묶이지 않습니다.
    async prepare() { await innertubeClient(); },
    proofStatus() { return proof?.status(); },
    async close() { await proof?.close(); },
  };
}
