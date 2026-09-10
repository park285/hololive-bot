/* eslint-disable */
/* tslint:disable */
// @ts-nocheck
/*
 * ---------------------------------------------------------------
 * ## THIS FILE WAS GENERATED VIA SWAGGER-TYPESCRIPT-API        ##
 * ##                                                           ##
 * ## AUTHOR: acacode                                           ##
 * ## SOURCE: https://github.com/acacode/swagger-typescript-api ##
 * ---------------------------------------------------------------
 */

export interface AddAliasRequest {
  alias: string;
  /** @example "ko" */
  type: string;
}

export interface AddMemberRequest {
  aliases: Aliases;
  channelId: string;
  isGraduated: boolean;
  name: string;
  nameJa?: string | null;
  nameKo?: string | null;
}

export interface AddRoomRequest {
  /** @minLength 1 */
  room: string;
}

export interface AdminMetadata {
  /** @pattern ^[a-f0-9]{64}$ */
  clientGeneration: string;
}

export interface AggregatedStatus {
  /** @format int64 */
  sampled_at: number;
  services: ServiceStatus[];
  uptime: string;
  version: string;
}

export interface Alarm {
  channelId: string;
  memberName: string;
  roomId: string;
  roomName: string;
}

export interface AlarmsResponse {
  alarms: Alarm[];
  status: "ok";
}

export interface Aliases {
  ja: string[];
  ko: string[];
}

export interface CalendarEntry {
  /** @format int32 */
  day: number;
  kind: string;
  member: CalendarMember;
  /** @format int32 */
  ordinal?: number | null;
}

export interface CalendarMember {
  channelId: string;
  /** @pattern ^[1-9][0-9]*$ */
  id: string;
  isGraduated?: boolean;
  name: string;
  nameKo?: string | null;
  org?: string | null;
  photo?: string | null;
  shortKoreanName?: string | null;
  suborg?: string | null;
}

export interface CalendarResponse {
  entries: CalendarEntry[];
  /** @format int32 */
  month: number;
  status: "ok";
  /** @format int32 */
  year: number;
}

export interface Container {
  /** @format int64 */
  created: number;
  health?: string | null;
  id: string;
  image: string;
  managed: boolean;
  name: string;
  ports: PortMapping[];
  state: string;
  status: string;
  stopBlocked: boolean;
}

export interface DeleteAlarmRequest {
  /** @minLength 1 */
  channelId: string;
  /** @minLength 1 */
  roomId: string;
}

export interface DeleteAlarmResponse {
  removed: boolean;
  status: "ok";
}

export interface DockerContainerListResponse {
  containers: Container[];
  status: "ok";
}

export interface DockerHealthResponse {
  available: boolean;
  status: "ok";
}

export interface ErrorResponse {
  absolute_expired?: boolean;
  /** @pattern ^[A-Z][A-Z0-9_]*$ */
  code: string;
  message: string;
  /**
   * Only the first atomic family claim may attest rejection before upstream dispatch. Must match the original X-Admin-Mutation-ID; absent evidence leaves the logical mutation outcome unknown.
   * @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$
   */
  notDispatchedMutationId?: string;
  /** @minLength 1 */
  requestId: string;
  /** @min 0 */
  retry_after?: number;
}

export interface HeartbeatRequest {
  idle?: boolean;
}

export type HeartbeatResponse =
  | {
      /** @min 0 */
      absolute_expires_at: number;
      status: "ok";
    }
  | {
      /** @min 0 */
      absolute_expires_at: number;
      /** @minLength 1 */
      csrf_token: string;
      rotated: true;
      status: "ok";
    }
  | {
      idle_rejected: true;
      status: "idle";
    };

export interface JoinedRoom {
  chatId: string;
  memberCount: number;
  name: string;
  type: string;
}

export interface JoinedRoomsResponse {
  rooms: JoinedRoom[];
  status: "ok";
}

export interface LoginRequest {
  password: string;
  username: string;
}

export interface LoginResponse {
  /** @minLength 1 */
  csrf_token: string;
  message: string;
  status: "ok";
}

export interface Member {
  aliases: Aliases;
  channelId: string;
  /** @pattern ^[1-9][0-9]*$ */
  id: string;
  isGraduated: boolean;
  name: string;
  nameJa?: string | null;
  nameKo?: string | null;
}

export interface MembersResponse {
  members: Member[];
  status: "ok";
}

export interface OpenAPIDocument {
  components: Record<string, any>;
  info: {
    title: string;
    version: string;
    [key: string]: any;
  };
  openapi: "3.1.0";
  paths: Record<string, object>;
  [key: string]: any;
}

export interface PortMapping {
  port_type: string;
  /**
   * @format int32
   * @min 0
   */
  private_port: number;
  /**
   * @format int32
   * @min 0
   */
  public_port?: number | null;
}

export interface RemoveAliasRequest {
  alias: string;
  /** @example "ja" */
  type: string;
}

export interface RemoveRoomRequest {
  /** @minLength 1 */
  room: string;
}

export interface RoomNameUpdateRequest {
  /** @minLength 1 */
  roomId: string;
  /** @minLength 1 */
  roomName: string;
}

export interface RoomsResponse {
  aclEnabled: boolean;
  aclMode: "whitelist" | "blacklist";
  rooms: string[];
  status: "ok";
}

export interface ServiceRuntimeStats {
  available: boolean;
  /** @min 0 */
  count: number;
  error?: string | null;
  metricKind: "goroutine" | "thread";
  name: string;
}

export interface ServiceStatus {
  available: boolean;
  error?: string | null;
  name: string;
  /**
   * @format int64
   * @min 0
   */
  response_time_ms?: number | null;
}

export interface SessionPolicyResponse {
  /**
   * @format int64
   * @min 0
   */
  absolute_warning_window_ms: number;
  /**
   * @format int64
   * @min 0
   */
  heartbeat_interval_ms: number;
  /**
   * @format int64
   * @min 0
   */
  idle_session_ttl_ms: number;
  /**
   * @format int64
   * @min 0
   */
  idle_timeout_ms: number;
  /**
   * @format int64
   * @min 0
   */
  idle_warning_timeout_ms: number;
}

export interface SessionStatusResponse {
  /** @format int64 */
  absolute_expires_at: number;
  authenticated: true;
  /** @minLength 1 */
  csrf_token: string;
  session_policy: SessionPolicyResponse;
  status: "ok";
  /** @minLength 1 */
  username: string;
}

export interface SetAclRequest {
  enabled?: boolean | null;
  mode?: string | null;
}

export interface SetAclResponse {
  enabled: boolean;
  mode: "whitelist" | "blacklist";
  status: "ok";
}

export interface SetGraduationRequest {
  isGraduated: boolean;
}

export interface Settings {
  /**
   * @min 0
   * @max 1440
   */
  alarmAdvanceMinutes: number;
}

export interface SettingsResponse {
  settings: Settings;
  status: "ok";
}

export interface SettingsRuntimeResult {
  alarm_applied?: boolean;
  alarm_reason?: string;
  alarm_requested_advance_minutes?: number;
  alarm_target_minutes?: number[];
  config_publish_alarm_advance_minutes?: boolean;
  config_publish_alarm_advance_minutes_error?: string;
  [key: string]: any;
}

export interface SettingsUpdateResponse {
  message: string;
  runtime: SettingsRuntimeResult;
  settings: Settings;
  status: "ok";
}

export interface StatsResponse {
  /** @format int32 */
  alarms: number;
  /** @format int32 */
  members: number;
  /** @format int32 */
  rooms: number;
  status: "ok";
  uptime: string;
  version: string;
}

export interface StatusOnlyResponse {
  message?: string | null;
  status: "ok";
}

export interface Stream {
  channel_id: string;
  channel_name?: string | null;
  id: string;
  link?: string | null;
  start_actual?: string | null;
  start_scheduled?: string | null;
  status: string;
  thumbnail?: string | null;
  title: string;
}

export interface StreamsResponse {
  org?: string | null;
  status: "ok";
  streams: Stream[];
}

export interface SystemStats {
  /** @min 0 */
  loadAvg1: number;
  /** @min 0 */
  loadAvg15: number;
  /** @min 0 */
  loadAvg5: number;
  /** @min 0 */
  cpuUsage: number;
  /** @min 0 */
  memoryTotal: number;
  /** @min 0 */
  memoryUsage: number;
  /** @min 0 */
  memoryUsed: number;
  /** @min 0 */
  sampledAt: number;
  serviceRuntime: ServiceRuntimeStats[];
  /** @min 0 */
  threadCount: number;
  /** @min 0 */
  totalGoGoroutines: number;
  /** @min 0 */
  totalRuntimeUnits: number;
}

export interface UpdateChannelRequest {
  /** @minLength 1 */
  channelId: string;
}

export interface UpdateMemberNameRequest {
  /** @minLength 1 */
  name: string;
}

export interface UserNameUpdateRequest {
  /** @minLength 1 */
  userId: string;
  /** @minLength 1 */
  userName: string;
}

export interface YouTubeCommunityShortsOpsChannel {
  /** @format int64 */
  alarmSentPostCount: number;
  /** @format int64 */
  averageLatencyMillis?: number | null;
  channelId: string;
  /** @format int64 */
  communityPostCount: number;
  /** @format int64 */
  detectedPostCount: number;
  /** @format int64 */
  detectedUnsentPostCount: number;
  earliestObservedAt?: string | null;
  /** @format int64 */
  exceededPostCount: number;
  /** @format int64 */
  failedPostCount: number;
  /** @format int64 */
  latencyMeasuredPostCount: number;
  latestObservedAt?: string | null;
  /** @format int64 */
  maxLatencyMillis?: number | null;
  memberName?: string | null;
  /** @format int64 */
  pendingPostCount: number;
  /** @format int64 */
  shortsPostCount: number;
  /** @format int64 */
  successPostCount: number;
  /** @format int64 */
  withinTargetPostCount: number;
}

export interface YouTubeCommunityShortsOpsOverview {
  /** @format int64 */
  alarmSentPostCount: number;
  /** @format int64 */
  averageLatencyMillis?: number | null;
  /** @format int64 */
  channelCount: number;
  /** @format int64 */
  communityDetectedPostCount: number;
  /** @format int64 */
  communityExceededPostCount: number;
  /** @format int64 */
  detectedPostCount: number;
  /** @format int64 */
  detectedUnsentPostCount: number;
  /** @format int64 */
  exceededPostCount: number;
  /** @format int64 */
  failedPostCount: number;
  /** @format int64 */
  latencyMeasuredPostCount: number;
  /** @format int64 */
  maxLatencyMillis?: number | null;
  /** @format int64 */
  pendingPostCount: number;
  /** @format int64 */
  shortsDetectedPostCount: number;
  /** @format int64 */
  shortsExceededPostCount: number;
  /** @format int64 */
  successPostCount: number;
  /** @format int64 */
  withinTargetPostCount: number;
}

export interface YouTubeCommunityShortsOpsResponse {
  channels: YouTubeCommunityShortsOpsChannel[];
  generatedAt: string;
  observedAtBasis: string;
  overview: YouTubeCommunityShortsOpsOverview;
  /** @format int64 */
  slaThresholdMillis: number;
  status: "ok";
  windowEnd: string;
  /** @format int64 */
  windowHours: number;
  windowStart: string;
}
