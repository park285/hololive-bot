// @ts-check
import { createInnertube, fetchCommunityFeed } from "./fetch-community.mjs";
import { fetchContentFeed } from "./fetch-content.mjs";
import { fetchChannelFeed } from "./fetch-channel.mjs";
import { FetchTransportError } from "./fetch-transport.mjs";
import { createLiveCheckInnertube, fetchChannelLiveCheck, fetchVideoLiveCheck } from "./live-check.mjs";
import { ProofController } from "./proof-controller.mjs";
import { ScheduleLookupCache } from "./schedule-cache.mjs";

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
  // 예정 영상 일정 확인은 helper 프로세스 수명 동안 채널 RPC 사이에서 재사용합니다.
  const scheduleCache = new ScheduleLookupCache();

  async function innertubeClient() {
    innertubePromise ??= initInnertube({ fetchImpl });
    return innertubePromise;
  }

  // 라이브 확인은 기존 수집 Innertube의 재시도 transport를 공유하지 않고, 단일 시도 transport에 묶인 별도 인스턴스를 씁니다.
  async function liveCheckInnertubeClient() {
    liveCheckInnertubePromise ??= initLiveCheckInnertube({ fetchImpl: singleAttemptFetchImpl }).then((client) => {
      const userAgent = client.session.user_agent;
      if (typeof userAgent !== "string" || userAgent !== client.session.context.client.userAgent) {
        throw new FetchTransportError(
          "helper_internal_invariant",
          "INTERNAL",
          "live check session user agent does not match its client context",
        );
      }
      // 단일 시도 transport 부재는 createLiveCheckInnertube와 ProofController 생성자가 각각 거부합니다.
      proof = new ProofController({ fetchImpl: /** @type {InnertubeFetch} */ (singleAttemptFetchImpl), userAgent });
      return client;
    });
    return liveCheckInnertubePromise;
  }

  return {
    async fetchCommunity(fetchOptions) {
      return fetchCommunityFeed({
        ...fetchOptions,
        innertube: await innertubeClient(),
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
        scheduleCache,
      });
    },
    async fetchChannelLiveCheck(fetchOptions) {
      return fetchChannelLiveCheck(await liveCheckInnertubeClient(), fetchOptions.channelId, Date.now, proof);
    },
    async fetchVideoLiveCheck(fetchOptions) {
      return fetchVideoLiveCheck(await liveCheckInnertubeClient(), fetchOptions.videoId, Date.now, proof);
    },
    // 수집·라이브 확인 Innertube와 PO 발급 controller는 bootstrap에서 요청 context 밖에 만듭니다. 첫 RPC의 취소·제한 시간에
    // 묶이지 않고, 생성 실패는 READY 전에 helper를 FAULTED로 끝내므로 실패한 생성을 RPC에서 다시 시도하지 않습니다.
    async prepare() {
      await innertubeClient();
      await liveCheckInnertubeClient();
    },
    proofStatus() { return proof?.status(); },
    async close() { await proof?.close(); },
  };
}
