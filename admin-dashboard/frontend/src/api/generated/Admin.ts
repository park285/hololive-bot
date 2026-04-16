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

import {
  AddAliasRequest,
  AddMemberRequest,
  AddRoomRequest,
  AggregatedStatus,
  AlarmsResponse,
  ChannelStatsResponse,
  DeleteAlarmRequest,
  DockerActionResponse,
  DockerContainerListResponse,
  DockerHealthResponse,
  ErrorResponse,
  HeartbeatRequest,
  HeartbeatResponse,
  LoginRequest,
  LoginResponse,
  MembersResponse,
  MilestoneStatsResponse,
  MilestonesResponse,
  NearMilestonesResponse,
  RemoveAliasRequest,
  RemoveRoomRequest,
  RoomNameUpdateRequest,
  RoomsResponse,
  SessionStatusResponse,
  SetAclRequest,
  SetAclResponse,
  SetGraduationRequest,
  Settings,
  SettingsResponse,
  StatsResponse,
  StatusOnlyResponse,
  StreamsResponse,
  UpdateChannelRequest,
  UpdateMemberNameRequest,
  UserNameUpdateRequest,
  YouTubeCommunityShortsOpsResponse,
} from "./data-contracts";
import { ContentType, HttpClient, RequestParams } from "./http-client";

export class Admin<
  SecurityDataType = unknown,
> extends HttpClient<SecurityDataType> {
  /**
   * No description
   *
   * @tags auth
   * @name HandleHeartbeat
   * @request POST:/admin/api/auth/heartbeat
   */
  handleHeartbeat = (data: HeartbeatRequest, params: RequestParams = {}) =>
    this.request<HeartbeatResponse, void>({
      path: `/admin/api/auth/heartbeat`,
      method: "POST",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags auth
   * @name HandleLogin
   * @request POST:/admin/api/auth/login
   */
  handleLogin = (data: LoginRequest, params: RequestParams = {}) =>
    this.request<LoginResponse, void>({
      path: `/admin/api/auth/login`,
      method: "POST",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags auth
   * @name HandleLogout
   * @request POST:/admin/api/auth/logout
   */
  handleLogout = (params: RequestParams = {}) =>
    this.request<void, any>({
      path: `/admin/api/auth/logout`,
      method: "POST",
      ...params,
    });
  /**
   * No description
   *
   * @tags auth
   * @name HandleSessionStatus
   * @request GET:/admin/api/auth/session
   */
  handleSessionStatus = (params: RequestParams = {}) =>
    this.request<SessionStatusResponse, void>({
      path: `/admin/api/auth/session`,
      method: "GET",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags docker
   * @name HandleDockerContainers
   * @request GET:/admin/api/docker/containers
   */
  handleDockerContainers = (params: RequestParams = {}) =>
    this.request<DockerContainerListResponse, void>({
      path: `/admin/api/docker/containers`,
      method: "GET",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags docker
   * @name HandleDockerRestart
   * @request POST:/admin/api/docker/containers/{name}/restart
   */
  handleDockerRestart = (name: string, params: RequestParams = {}) =>
    this.request<DockerActionResponse, void>({
      path: `/admin/api/docker/containers/${name}/restart`,
      method: "POST",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags docker
   * @name HandleDockerStart
   * @request POST:/admin/api/docker/containers/{name}/start
   */
  handleDockerStart = (name: string, params: RequestParams = {}) =>
    this.request<DockerActionResponse, void>({
      path: `/admin/api/docker/containers/${name}/start`,
      method: "POST",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags docker
   * @name HandleDockerStop
   * @request POST:/admin/api/docker/containers/{name}/stop
   */
  handleDockerStop = (name: string, params: RequestParams = {}) =>
    this.request<DockerActionResponse, void>({
      path: `/admin/api/docker/containers/${name}/stop`,
      method: "POST",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags docker
   * @name HandleDockerHealth
   * @request GET:/admin/api/docker/health
   */
  handleDockerHealth = (params: RequestParams = {}) =>
    this.request<DockerHealthResponse, any>({
      path: `/admin/api/docker/health`,
      method: "GET",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloGetAlarms
   * @request GET:/admin/api/holo/alarms
   */
  holoGetAlarms = (params: RequestParams = {}) =>
    this.request<AlarmsResponse, ErrorResponse>({
      path: `/admin/api/holo/alarms`,
      method: "GET",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloDeleteAlarm
   * @request DELETE:/admin/api/holo/alarms
   */
  holoDeleteAlarm = (data: DeleteAlarmRequest, params: RequestParams = {}) =>
    this.request<StatusOnlyResponse, ErrorResponse>({
      path: `/admin/api/holo/alarms`,
      method: "DELETE",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloGetMembers
   * @request GET:/admin/api/holo/members
   */
  holoGetMembers = (params: RequestParams = {}) =>
    this.request<MembersResponse, ErrorResponse>({
      path: `/admin/api/holo/members`,
      method: "GET",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloAddMember
   * @request POST:/admin/api/holo/members
   */
  holoAddMember = (data: AddMemberRequest, params: RequestParams = {}) =>
    this.request<StatusOnlyResponse, ErrorResponse>({
      path: `/admin/api/holo/members`,
      method: "POST",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloAddAlias
   * @request POST:/admin/api/holo/members/{id}/aliases
   */
  holoAddAlias = (
    id: number,
    data: AddAliasRequest,
    params: RequestParams = {},
  ) =>
    this.request<StatusOnlyResponse, ErrorResponse>({
      path: `/admin/api/holo/members/${id}/aliases`,
      method: "POST",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloRemoveAlias
   * @request DELETE:/admin/api/holo/members/{id}/aliases
   */
  holoRemoveAlias = (
    id: number,
    data: RemoveAliasRequest,
    params: RequestParams = {},
  ) =>
    this.request<StatusOnlyResponse, ErrorResponse>({
      path: `/admin/api/holo/members/${id}/aliases`,
      method: "DELETE",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloUpdateChannel
   * @request PATCH:/admin/api/holo/members/{id}/channel
   */
  holoUpdateChannel = (
    id: number,
    data: UpdateChannelRequest,
    params: RequestParams = {},
  ) =>
    this.request<StatusOnlyResponse, ErrorResponse>({
      path: `/admin/api/holo/members/${id}/channel`,
      method: "PATCH",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloSetGraduation
   * @request PATCH:/admin/api/holo/members/{id}/graduation
   */
  holoSetGraduation = (
    id: number,
    data: SetGraduationRequest,
    params: RequestParams = {},
  ) =>
    this.request<StatusOnlyResponse, ErrorResponse>({
      path: `/admin/api/holo/members/${id}/graduation`,
      method: "PATCH",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloUpdateMemberName
   * @request PATCH:/admin/api/holo/members/{id}/name
   */
  holoUpdateMemberName = (
    id: number,
    data: UpdateMemberNameRequest,
    params: RequestParams = {},
  ) =>
    this.request<StatusOnlyResponse, ErrorResponse>({
      path: `/admin/api/holo/members/${id}/name`,
      method: "PATCH",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloGetMilestones
   * @request GET:/admin/api/holo/milestones
   */
  holoGetMilestones = (
    query?: {
      /** @format int64 */
      limit?: number | null;
      /** @format int64 */
      offset?: number | null;
      channelId?: string | null;
      memberName?: string | null;
    },
    params: RequestParams = {},
  ) =>
    this.request<MilestonesResponse, ErrorResponse>({
      path: `/admin/api/holo/milestones`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloGetNearMilestones
   * @request GET:/admin/api/holo/milestones/near
   */
  holoGetNearMilestones = (
    query?: {
      /** @format double */
      threshold?: number | null;
    },
    params: RequestParams = {},
  ) =>
    this.request<NearMilestonesResponse, ErrorResponse>({
      path: `/admin/api/holo/milestones/near`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloGetMilestoneStats
   * @request GET:/admin/api/holo/milestones/stats
   */
  holoGetMilestoneStats = (params: RequestParams = {}) =>
    this.request<MilestoneStatsResponse, ErrorResponse>({
      path: `/admin/api/holo/milestones/stats`,
      method: "GET",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloSetRoomName
   * @request POST:/admin/api/holo/names/room
   */
  holoSetRoomName = (data: RoomNameUpdateRequest, params: RequestParams = {}) =>
    this.request<StatusOnlyResponse, ErrorResponse>({
      path: `/admin/api/holo/names/room`,
      method: "POST",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloSetUserName
   * @request POST:/admin/api/holo/names/user
   */
  holoSetUserName = (data: UserNameUpdateRequest, params: RequestParams = {}) =>
    this.request<StatusOnlyResponse, ErrorResponse>({
      path: `/admin/api/holo/names/user`,
      method: "POST",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloGetRooms
   * @request GET:/admin/api/holo/rooms
   */
  holoGetRooms = (params: RequestParams = {}) =>
    this.request<RoomsResponse, ErrorResponse>({
      path: `/admin/api/holo/rooms`,
      method: "GET",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloAddRoom
   * @request POST:/admin/api/holo/rooms
   */
  holoAddRoom = (data: AddRoomRequest, params: RequestParams = {}) =>
    this.request<StatusOnlyResponse, ErrorResponse>({
      path: `/admin/api/holo/rooms`,
      method: "POST",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloRemoveRoom
   * @request DELETE:/admin/api/holo/rooms
   */
  holoRemoveRoom = (data: RemoveRoomRequest, params: RequestParams = {}) =>
    this.request<StatusOnlyResponse, ErrorResponse>({
      path: `/admin/api/holo/rooms`,
      method: "DELETE",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloSetAcl
   * @request POST:/admin/api/holo/rooms/acl
   */
  holoSetAcl = (data: SetAclRequest, params: RequestParams = {}) =>
    this.request<SetAclResponse, ErrorResponse>({
      path: `/admin/api/holo/rooms/acl`,
      method: "POST",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloGetSettings
   * @request GET:/admin/api/holo/settings
   */
  holoGetSettings = (params: RequestParams = {}) =>
    this.request<SettingsResponse, ErrorResponse>({
      path: `/admin/api/holo/settings`,
      method: "GET",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloUpdateSettings
   * @request POST:/admin/api/holo/settings
   */
  holoUpdateSettings = (data: Settings, params: RequestParams = {}) =>
    this.request<StatusOnlyResponse, ErrorResponse>({
      path: `/admin/api/holo/settings`,
      method: "POST",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloGetStats
   * @request GET:/admin/api/holo/stats
   */
  holoGetStats = (params: RequestParams = {}) =>
    this.request<StatsResponse, ErrorResponse>({
      path: `/admin/api/holo/stats`,
      method: "GET",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloGetChannelStats
   * @request GET:/admin/api/holo/stats/channels
   */
  holoGetChannelStats = (
    query?: {
      /** @min 0 */
      limit?: number | null;
    },
    params: RequestParams = {},
  ) =>
    this.request<ChannelStatsResponse, ErrorResponse>({
      path: `/admin/api/holo/stats/channels`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloGetYouTubeCommunityShortsOps
   * @request GET:/admin/api/holo/stats/youtube/community-shorts
   */
  holoGetYouTubeCommunityShortsOps = (params: RequestParams = {}) =>
    this.request<YouTubeCommunityShortsOpsResponse, ErrorResponse>({
      path: `/admin/api/holo/stats/youtube/community-shorts`,
      method: "GET",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloGetLiveStreams
   * @request GET:/admin/api/holo/streams/live
   */
  holoGetLiveStreams = (
    query?: {
      org?: string | null;
    },
    params: RequestParams = {},
  ) =>
    this.request<StreamsResponse, ErrorResponse>({
      path: `/admin/api/holo/streams/live`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloGetUpcomingStreams
   * @request GET:/admin/api/holo/streams/upcoming
   */
  holoGetUpcomingStreams = (
    query?: {
      org?: string | null;
    },
    params: RequestParams = {},
  ) =>
    this.request<StreamsResponse, ErrorResponse>({
      path: `/admin/api/holo/streams/upcoming`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags status
   * @name HandleAggregatedStatus
   * @request GET:/admin/api/status
   */
  handleAggregatedStatus = (params: RequestParams = {}) =>
    this.request<AggregatedStatus, any>({
      path: `/admin/api/status`,
      method: "GET",
      format: "json",
      ...params,
    });
}
