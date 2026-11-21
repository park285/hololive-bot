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
 * @param {{ fetchImpl?: InnertubeFetch, singleAttemptFetchImpl?: InnertubeFetch, createInnertubeImpl?: typeof createInnertube, createLiveCheckInnertubeImpl?: typeof createLiveCheckInnertube, }} [options]
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
  const scheduleCache = new ScheduleLookupCache();

  async function innertubeClient() {
    innertubePromise ??= initInnertube({ fetchImpl });
    return innertubePromise;
  }

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
    async prepare() {
      await innertubeClient();
      await liveCheckInnertubeClient();
    },
    proofStatus() { return proof?.status(); },
    async close() { await proof?.close(); },
  };
}
