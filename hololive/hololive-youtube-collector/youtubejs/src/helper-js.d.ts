interface Error {
  code?: string;
}

interface YouTubeJSFetchOptions {
  channelId?: string;
  videoId?: string;
  kind?: string;
  maxResults?: number;
  maxPages?: number;
  maxAggregateBytes?: number;
  innertube?: any;
  postType?: any;
  fetchImpl?: any;
}
