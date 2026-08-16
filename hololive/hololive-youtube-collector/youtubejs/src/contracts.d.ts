export type Awaitable<T> = T | Promise<T>;

export type Continuity = "CONTIGUOUS" | "GAP_UNRESOLVED" | "NOT_APPLICABLE";
export type ViewerAvailability = "AVAILABLE" | "HIDDEN" | "UNAVAILABLE";
export type LiveStatus = "LIVE" | "UPCOMING" | "ENDED" | "CANCELLED";
export type PhotoKind = "avatar" | "banner";
export type ContentKind = "videos" | "shorts";

export interface Pagination {
  page_count: number;
  cursor_start?: string;
  cursor_end?: string;
  exhausted: boolean;
  continuity: Continuity;
}

export interface CommunityRequest {
  channel_id: string;
  max_results?: number;
  max_pages?: number;
  max_aggregate_bytes?: number;
  proxy_url?: string;
}

export interface ContentRequest extends CommunityRequest {
  kind: ContentKind;
}

export interface ChannelRequest {
  channel_id: string;
  max_pages?: number;
  max_aggregate_bytes?: number;
  proxy_url?: string;
}

export interface ViewerRequest {
  video_id: string;
  max_aggregate_bytes?: number;
  proxy_url?: string;
}

export interface CommunityFetchOptions {
  channelId: string;
  maxResults?: number;
  maxPages?: number;
  maxAggregateBytes?: number;
}

export interface ContentFetchOptions extends CommunityFetchOptions {
  kind: ContentKind;
}

export interface ChannelFetchOptions {
  channelId: string;
  maxPages?: number;
  maxAggregateBytes?: number;
}

export interface ViewerFetchOptions {
  videoId: string;
  maxAggregateBytes?: number;
}

export type CommunityFetcher = (options: CommunityFetchOptions) => Awaitable<CommunityResult>;
export type ContentFetcher = (options: ContentFetchOptions) => Awaitable<ContentResult>;
export type ChannelFetcher = (options: ChannelFetchOptions) => Awaitable<ChannelResult>;
export type ViewerFetcher = (options: ViewerFetchOptions) => Awaitable<ViewerResult>;
export type ProxyConfigurator = (url: string | undefined) => void;

export interface FetcherSet {
  fetchCommunity: CommunityFetcher;
  fetchContent: ContentFetcher;
  fetchChannel: ChannelFetcher;
  fetchViewer: ViewerFetcher;
}

export type RpcFetchers = FetcherSet;

export interface RpcEndpoint<Request, Response> {
  validateRequest: (value: unknown) => Request;
  validateResponse: (value: unknown) => Response;
}

export interface HelperErrorBody {
  error: string;
  error_code: string;
  error_class: string;
}

export interface CommunityResult extends Pagination {
  posts: CommunityPost[];
  missing_tab?: boolean;
  error?: string;
}

export interface Thumbnail {
  url: string;
  width: number;
  height: number;
}

export interface CommunityPost {
  postId: string;
  upstreamPostId?: string;
  authorId: string;
  authorName: string;
  authorPhoto: Thumbnail[];
  contentText: string;
  publishedText: string;
  publishedAt?: string;
  likeCount: number;
  commentCount: number;
  images?: Thumbnail[];
  videoId?: string;
}

export interface ContentItem {
  video_id: string;
  channel_id: string;
  title: string;
  published_at?: string;
  scheduled_for?: string;
}

export interface ContentResult extends Pagination {
  items: ContentItem[];
  missing_tab?: boolean;
  error?: string;
}

export interface LiveSessionItem {
  video_id: string;
  channel_id: string;
  status: LiveStatus;
  scheduled_at?: string;
  started_at?: string;
  ended_at?: string;
}

export interface ChannelStatsItem {
  subscriber_count?: number | null;
  view_count?: number | null;
  video_count?: number | null;
}

export interface ChannelProfileItem {
  handle?: string | null;
  description?: string | null;
  country?: string | null;
  joined_date?: string | null;
}

export interface ChannelPhotoVariant {
  kind: PhotoKind;
  url: string;
  width: number;
  height: number;
}

export interface ChannelResult extends Pagination {
  live_sessions: LiveSessionItem[];
  stats: ChannelStatsItem;
  profile: ChannelProfileItem;
  photo: ChannelPhotoVariant[];
  missing_tab?: boolean;
  error?: string;
}

export interface ViewerResult extends Pagination {
  video_id: string;
  viewer_count?: number | null;
  availability: ViewerAvailability;
  error?: string;
}
