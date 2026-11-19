export type Awaitable<T> = T | Promise<T>;

export type RuntimeState =
  | "UNCONFIGURED"
  | "READY"
  | "DRAINING"
  | "STOPPED"
  | "FAULTED";

export type ProofState = "COLD" | "WARMING" | "READY" | "EXPIRED" | "UNAVAILABLE" | "STOPPED";

export interface ProofStatus {
  state: ProofState;
  generation?: string;
  expires_at?: string;
  next_attempt_at?: string;
  last_error?: string;
  cleanup_error?: string;
  bootstrap_attempts: number;
  bootstrap_successes: number;
  upstream_requests: number;
  minted_total: number;
  attached_total: number;
}

export type Continuity = "CONTIGUOUS" | "GAP_UNRESOLVED" | "NOT_APPLICABLE";
export type LiveStatus = "LIVE" | "UPCOMING" | "ENDED";
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
  exhausted: boolean;
  continuity: Continuity;
  termination_reason: TerminationReason;
}

export interface CommunityRequest {
  protocol_version: number;
  channel_id: string;
  max_results: number;
  max_pages: number;
  max_success_response_bytes: number;
}

export interface ContentRequest extends CommunityRequest {
  kind: ContentKind;
}

export interface ChannelRequest {
  protocol_version: number;
  channel_id: string;
  kind: "live" | "metadata";
  max_schedule_lookups?: number;
  scheduled_for?: string;
  max_success_response_bytes: number;
}

export interface CommunityFetchOptions {
  channelId: string;
  maxResults: number;
  maxPages: number;
  maxSuccessResponseBytes: number;
}

export interface ContentFetchOptions extends CommunityFetchOptions {
  kind: ContentKind;
}

export interface ChannelFetchOptions {
  channelId: string;
  kind: "live" | "metadata";
  maxScheduleLookups?: number;
  scheduledFor?: string;
  maxSuccessResponseBytes: number;
}

export interface ChannelLiveCheckRequest {
  protocol_version: number;
  channel_id: string;
  max_success_response_bytes: number;
}

export interface VideoLiveCheckRequest {
  protocol_version: number;
  video_id: string;
  max_success_response_bytes: number;
}

export interface ChannelLiveCheckFetchOptions {
  channelId: string;
  maxSuccessResponseBytes: number;
}

export interface VideoLiveCheckFetchOptions {
  videoId: string;
  maxSuccessResponseBytes: number;
}

export type CommunityFetcher = (options: CommunityFetchOptions) => Awaitable<Omit<CommunityResult, "protocol_version">>;
export type ContentFetcher = (options: ContentFetchOptions) => Awaitable<Omit<ContentResult, "protocol_version">>;
export type ChannelFetcher = (options: ChannelFetchOptions) => Awaitable<Omit<ChannelResult, "protocol_version">>;
export type ChannelLiveCheckFetcher = (
  options: ChannelLiveCheckFetchOptions,
) => Awaitable<Omit<ChannelLiveCheckResult, "protocol_version">>;
export type VideoLiveCheckFetcher = (
  options: VideoLiveCheckFetchOptions,
) => Awaitable<Omit<VideoLiveCheckResult, "protocol_version">>;

export interface FetcherSet {
  fetchCommunity: CommunityFetcher;
  fetchContent: ContentFetcher;
  fetchChannel: ChannelFetcher;
  fetchChannelLiveCheck: ChannelLiveCheckFetcher;
  fetchVideoLiveCheck: VideoLiveCheckFetcher;
  prepare?: () => Promise<void>;
  proofStatus?: () => ProofStatus | undefined;
  close?: () => Awaitable<void>;
}

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
  schedule_lookups?: number;
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
  is_upcoming?: true;
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
  title?: string;
  thumbnail_url?: string;
  scheduled_at?: string;
  schedule_observed_at?: string;
  started_at?: string;
  ended_at?: string;
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
  unavailable_live_sessions?: UnavailableLiveSession[];
  profile: ChannelProfileItem;
  photo: ChannelPhotoVariant[];
  missing_tab?: boolean;
  schedule_lookups?: number;
  live_query?: {
    channel_id: string;
    source: "streams";
    statuses: string[];
    exhausted: boolean;
    access_restricted: boolean;
    page_count: number;
  };
}

export interface UnavailableLiveSession {
  video_id: string;
  channel_id: string;
  reason: "access_restricted";
}

export type ChannelLiveCheckOutcome = "LIVE_VIDEO" | "UPCOMING_VIDEO" | "CHANNEL_PAGE" | "UNKNOWN";
export type VideoAvailability = "PUBLIC" | "MEMBERS_ONLY" | "PUBLIC_UNAVAILABLE" | "UNKNOWN";
export type VideoAvailabilityMethod = "player_public" | "player_members_only" | "player_private" | "unknown";

export type ChannelLiveCheckUnknownReason =
  | "identity_missing"
  | "identity_mismatch"
  | "contradictory_fields"
  | "structure_unrecognized"
  | "not_waiting_state"
  | "login_required_unclassified"
  | "error_unclassified";

export type VideoLiveCheckUnknownReason =
  | "identity_missing"
  | "identity_mismatch"
  | "contradictory_fields"
  | "structure_unrecognized"
  | "login_required_unclassified"
  | "error_unclassified"
  | "availability_unclassified";

export interface ChannelLiveCheckResult {
  protocol_version: number;
  channel_id: string;
  outcome: ChannelLiveCheckOutcome;
  selected_video_id?: string;
  channel_identity_confirmed: boolean;
  unknown_reason?: ChannelLiveCheckUnknownReason;
}

export interface VideoLiveCheckResult {
  protocol_version: number;
  video_id: string;
  channel_id?: string;
  identity_confirmed: boolean;
  is_live?: boolean;
  is_live_now?: boolean;
  is_upcoming?: boolean;
  is_live_content?: boolean;
  is_private?: boolean;
  has_live_broadcast_details?: boolean;
  started_at?: string;
  scheduled_at?: string;
  waiting_state_confirmed?: boolean;
  ended_at?: string;
  published_at?: string;
  availability: VideoAvailability;
  method: VideoAvailabilityMethod;
  unknown_reason?: VideoLiveCheckUnknownReason;
}
