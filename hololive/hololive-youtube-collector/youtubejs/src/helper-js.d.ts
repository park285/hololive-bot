interface Error {
  code?: string;
}

interface YouTubeJSFetchOptions {
  channelId?: string;
  kind?: string;
  maxResults?: number;
  maxPages?: number;
  maxScheduleLookups?: number;
  maxSuccessResponseBytes?: number;
  innertube?: any;
  scheduleCache?: import("./schedule-cache.mjs").ScheduleLookupCache;
  postType?: any;
  fetchImpl?: any;
}
