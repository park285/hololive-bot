/** @typedef {import("../contracts.d.ts").FetcherSet} FetcherSet */

// helper runtime 단위 테스트용 fetcher입니다. 수집 결과를 만들지 않으므로 production 진입점은 이 파일을 import하지 않습니다.
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
      profile: {},
      photo: [],
      page_count: 1,
      exhausted: true,
      continuity: "NOT_APPLICABLE",
      termination_reason: "exhausted",
    };
  },
  // 테스트 fetcher에는 upstream 응답이 없으므로 확인 결과를 음성이나 가용성으로 만들지 않습니다.
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
