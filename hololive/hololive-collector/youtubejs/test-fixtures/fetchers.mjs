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
