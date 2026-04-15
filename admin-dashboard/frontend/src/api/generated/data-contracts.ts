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
  room: string;
}

export interface AggregatedStatus {
  services: ServiceStatus[];
  uptime: string;
  version: string;
}

export interface Alarm {
  channelId: string;
  memberName: string;
  roomId: string;
  roomName: string;
  userId: string;
  userName: string;
}

export interface AlarmsResponse {
  alarms: Alarm[];
  status: string;
}

export interface Aliases {
  ja: string[];
  ko: string[];
}

export interface ChannelStat {
  ChannelID: string;
  ChannelTitle: string;
  /** @format int64 */
  SubscriberCount: number;
  /** @format int64 */
  VideoCount: number;
  /** @format int64 */
  ViewCount: number;
}

export interface ChannelStatsResponse {
  stats: Partial<Record<string, ChannelStat>>;
  status: string;
}

export interface Container {
  /** @format int64 */
  created: number;
  health?: string | null;
  id: string;
  image: string;
  name: string;
  ports: PortMapping[];
  state: string;
  status: string;
}

export interface DeleteAlarmRequest {
  channelId: string;
  roomId: string;
  userId: string;
}

export interface DockerActionResponse {
  message: string;
  status: string;
}

export interface DockerContainerListResponse {
  containers: Container[];
  status: string;
}

export interface DockerHealthResponse {
  available: boolean;
  status: string;
}

export interface ErrorResponse {
  absolute_expired?: boolean | null;
  code?: string | null;
  details?: any;
  error: string;
  /**
   * @format int64
   * @min 0
   */
  retry_after?: number | null;
}

export interface HeartbeatRequest {
  idle?: boolean;
}

export interface HeartbeatResponse {
  csrf_token?: string | null;
  idle_rejected?: boolean | null;
  rotated?: boolean | null;
  status: string;
}

export interface LoginRequest {
  password: string;
  username: string;
}

export interface LoginResponse {
  csrf_token: string;
  message: string;
  status: string;
}

export interface Member {
  aliases: Aliases;
  channelId: string;
  /** @format int64 */
  id: number;
  isGraduated: boolean;
  name: string;
  nameJa?: string | null;
  nameKo?: string | null;
}

export interface MembersResponse {
  members: Member[];
  status: string;
}

export interface Milestone {
  achievedAt: string;
  channelId: string;
  memberName: string;
  notified: boolean;
  type: string;
  /** @format int64 */
  value: number;
}

export interface MilestoneStats {
  /** @format int64 */
  notNotifiedCount: number;
  /** @format int64 */
  recentAchievements: number;
  /** @format int64 */
  totalAchieved: number;
  /** @format int64 */
  totalNearMilestone: number;
}

export interface MilestoneStatsResponse {
  stats: MilestoneStats;
  status: string;
}

export interface MilestonesResponse {
  /** @format int64 */
  limit: number;
  milestones: Milestone[];
  /** @format int64 */
  offset: number;
  status: string;
  /** @format int64 */
  total: number;
}

export interface NearMilestone {
  channelId: string;
  /** @format int64 */
  currentSubs: number;
  memberName: string;
  /** @format int64 */
  nextMilestone: number;
  /** @format double */
  progressPct: number;
  /** @format int64 */
  remaining: number;
}

export interface NearMilestonesResponse {
  /** @format int64 */
  count: number;
  members: NearMilestone[];
  status: string;
  /** @format double */
  threshold: number;
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
  room: string;
}

export interface RoomNameUpdateRequest {
  roomId: string;
  roomName: string;
}

export interface RoomsResponse {
  aclEnabled: boolean;
  aclMode: string;
  rooms: string[];
  status: string;
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

export interface SessionStatusResponse {
  authenticated: boolean;
  status: string;
  username: string;
}

export interface SetAclRequest {
  enabled?: boolean | null;
  mode?: string | null;
}

export interface SetAclResponse {
  enabled: boolean;
  mode: string;
  status: string;
}

export interface SetGraduationRequest {
  isGraduated: boolean;
}

export interface Settings {
  /** @format int32 */
  alarmAdvanceMinutes: number;
}

export interface SettingsResponse {
  settings: Settings;
  status: string;
}

export interface StatsResponse {
  /** @format int32 */
  alarms: number;
  /** @format int32 */
  members: number;
  /** @format int32 */
  rooms: number;
  status: string;
  uptime: string;
  version: string;
}

export interface StatusOnlyResponse {
  message?: string | null;
  status: string;
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
  status: string;
  streams: Stream[];
}

export interface UpdateChannelRequest {
  channelId: string;
}

export interface UpdateMemberNameRequest {
  name: string;
}

export interface UserNameUpdateRequest {
  userId: string;
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
  status: string;
  windowEnd: string;
  /** @format int64 */
  windowHours: number;
  windowStart: string;
}
