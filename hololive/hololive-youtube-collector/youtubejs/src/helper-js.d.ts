interface Error {
  code?: string;
}

interface YouTubeJSFetchOptions {
  channelId?: string;
  videoId?: string;
  kind?: string;
  maxResults?: number;
  maxPages?: number;
  maxSuccessResponseBytes?: number;
  innertube?: any;
  postType?: any;
  fetchImpl?: any;
}
