export type Awaitable<T> = T | Promise<T>;

export type RuntimeState =
  | "UNCONFIGURED"
  | "READY"
  | "DRAINING"
  | "STOPPED"
  | "FAULTED";

export interface BootstrapProxy {
  enabled: boolean;
  url?: string;
}

export interface BootstrapLimits {
  request_body_bytes: number;
  response_body_bytes: number;
  max_inflight: number;
}

export interface BootstrapRequest {
  protocol_version: number;
  proxy: BootstrapProxy;
  limits: BootstrapLimits;
}

export interface BootstrapResponse {
  protocol_version: number;
  state: RuntimeState;
  proxy_enabled: boolean;
  request_body_bytes: number;
  response_body_bytes: number;
  max_inflight: number;
}

export interface HealthResponse {
  protocol_version: number;
  state: RuntimeState;
  inflight: number;
  max_inflight: number;
  proxy_enabled: boolean;
}

export type Continuity = "CONTIGUOUS" | "GAP_UNRESOLVED" | "NOT_APPLICABLE";
export type ViewerAvailability = "AVAILABLE" | "HIDDEN" | "UNAVAILABLE";
export type LiveStatus = "LIVE" | "UPCOMING" | "ENDED" | "CANCELLED";
export type PhotoKind = "avatar" | "banner";
export type ContentKind = "videos" | "shorts";
export type TerminationReason =
  | "exhausted"
  | "max_pages"
  | "max_results"
  | "max_success_response_bytes"
  | "cursor_loop"
  | "continuation_transient";

export interface Pagination {
  page_count: number;
  cursor_start?: string;
  cursor_end?: string;
  exhausted: boolean;
  continuity: Continuity;
  termination_reason: TerminationReason;
}

export interface CommunityRequest {
  protocol_version: number;
  channel_id: string;
  max_results?: number;
  max_pages?: number;
  max_success_response_bytes: number;
}

export interface ContentRequest extends CommunityRequest {
  kind: ContentKind;
}

export interface ChannelRequest {
  protocol_version: number;
  channel_id: string;
  max_pages?: number;
  max_success_response_bytes: number;
}

export interface ViewerRequest {
  protocol_version: number;
  video_id: string;
  max_success_response_bytes: number;
}

export interface CommunityFetchOptions {
  channelId: string;
  maxResults?: number;
  maxPages?: number;
  maxSuccessResponseBytes: number;
}

export interface ContentFetchOptions extends CommunityFetchOptions {
  kind: ContentKind;
}

export interface ChannelFetchOptions {
  channelId: string;
  maxPages?: number;
  maxSuccessResponseBytes: number;
}

export interface ViewerFetchOptions {
  videoId: string;
  maxSuccessResponseBytes: number;
}

export type CommunityFetcher = (options: CommunityFetchOptions) => Awaitable<Omit<CommunityResult, "protocol_version">>;
export type ContentFetcher = (options: ContentFetchOptions) => Awaitable<Omit<ContentResult, "protocol_version">>;
export type ChannelFetcher = (options: ChannelFetchOptions) => Awaitable<Omit<ChannelResult, "protocol_version">>;
export type ViewerFetcher = (options: ViewerFetchOptions) => Awaitable<Omit<ViewerResult, "protocol_version">>;

export interface FetcherSet {
  fetchCommunity: CommunityFetcher;
  fetchContent: ContentFetcher;
  fetchChannel: ChannelFetcher;
  fetchViewer: ViewerFetcher;
  close?: () => Awaitable<void>;
}

export type RpcFetchers = FetcherSet;

export interface RpcEndpoint<Request, Response> {
  validateRequest: (value: unknown) => Request;
  validateResponse: (value: unknown) => Response;
  minimumSuccessResponseBytes: number;
}

export interface ProtocolMeta {
  protocol_version: number;
}

export type RPCErrorCode =
  | "invalid_request"
  | "request_too_large"
  | "helper_not_ready"
  | "helper_busy"
  | "collection_canceled"
  | "collection_timeout"
  | "cooldown"
  | "parser_drift"
  | "configuration_error"
  | "response_too_large"
  | "helper_protocol_mismatch"
  | "helper_internal_invariant"
  | "collection_failed";

export type RPCFailureClass =
  | "TRANSIENT"
  | "TIMEOUT"
  | "CANCELED"
  | "COOLDOWN"
  | "DATA_CONTRACT"
  | "RESOURCE_LIMIT"
  | "CONFIGURATION"
  | "PROTOCOL"
  | "INTERNAL";

export type RPCRetryKind = "default" | "after" | "at";

export interface RPCRetryHint {
  kind: RPCRetryKind;
  after_ms?: number;
  at?: string;
}

export interface RPCFailure {
  code: RPCErrorCode;
  class: RPCFailureClass;
  retry: RPCRetryHint;
  message: string;
}

export interface RPCErrorBody extends ProtocolMeta {
  error: RPCFailure;
}

export interface CommunityResult extends Pagination {
  protocol_version: number;
  posts: CommunityPost[];
  missing_tab?: boolean;
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
  is_premiere?: boolean;
}

export interface ContentResult extends Pagination {
  protocol_version: number;
  items: ContentItem[];
  missing_tab?: boolean;
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
  protocol_version: number;
  live_sessions: LiveSessionItem[];
  stats: ChannelStatsItem;
  profile: ChannelProfileItem;
  photo: ChannelPhotoVariant[];
  missing_tab?: boolean;
}

export interface ViewerResult extends Pagination {
  protocol_version: number;
  video_id: string;
  viewer_count?: number | null;
  availability: ViewerAvailability;
}
