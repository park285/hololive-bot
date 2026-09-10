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

import type {
  AddAliasRequest,
  AddMemberRequest,
  AddRoomRequest,
  AdminMetadata,
  AggregatedStatus,
  AlarmsResponse,
  CalendarResponse,
  DeleteAlarmRequest,
  DeleteAlarmResponse,
  DockerContainerListResponse,
  DockerHealthResponse,
  HeartbeatRequest,
  HeartbeatResponse,
  JoinedRoomsResponse,
  LoginRequest,
  LoginResponse,
  MembersResponse,
  OpenAPIDocument,
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
  SettingsUpdateResponse,
  StatsResponse,
  StatusOnlyResponse,
  StreamsResponse,
  UpdateChannelRequest,
  UpdateMemberNameRequest,
  UserNameUpdateRequest,
  YouTubeCommunityShortsOpsResponse,
} from "./data-contracts";
import type { HttpClient, RequestParams } from "./http-client";
import { ContentType } from "./http-client";

/** 생성 시 주입한 단일 transport로 관리자 계약을 호출합니다. */
export class Admin {
  private readonly http: HttpClient;
  constructor(http: HttpClient) {
    this.http = http;
  }
  /**
   * No description
   *
   * @tags admin
   * @name GetAdminMetadata
   * @request GET:/admin/meta.json
   */
  getAdminMetadata = (params: RequestParams = {}) =>
    this.http.request<AdminMetadata>({
      operationId: "getAdminMetadata",
      path: `/admin/meta.json`,
      method: "GET",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags admin
   * @name GetAdminOpenApi
   * @request GET:/admin/api/openapi.json
   */
  getAdminOpenApi = (params: RequestParams = {}) =>
    this.http.request<OpenAPIDocument>({
      operationId: "getAdminOpenAPI",
      path: `/admin/api/openapi.json`,
      method: "GET",
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
    this.http.request<AggregatedStatus>({
      operationId: "handle_aggregated_status",
      path: `/admin/api/status`,
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
    this.http.request<DockerContainerListResponse>({
      operationId: "handle_docker_containers",
      path: `/admin/api/docker/containers`,
      method: "GET",
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
    this.http.request<DockerHealthResponse>({
      operationId: "handle_docker_health",
      path: `/admin/api/docker/health`,
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
    this.http.request<StatusOnlyResponse>({
      operationId: "handle_docker_restart",
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
    this.http.request<StatusOnlyResponse>({
      operationId: "handle_docker_start",
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
    this.http.request<StatusOnlyResponse>({
      operationId: "handle_docker_stop",
      path: `/admin/api/docker/containers/${name}/stop`,
      method: "POST",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags auth
   * @name HandleHeartbeat
   * @request POST:/admin/api/auth/heartbeat
   */
  handleHeartbeat = (data?: HeartbeatRequest, params: RequestParams = {}) =>
    this.http.request<HeartbeatResponse>({
      operationId: "handle_heartbeat",
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
    this.http.request<LoginResponse>({
      operationId: "handle_login",
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
    this.http.request<StatusOnlyResponse>({
      operationId: "handle_logout",
      path: `/admin/api/auth/logout`,
      method: "POST",
      format: "json",
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
    this.http.request<SessionStatusResponse>({
      operationId: "handle_session_status",
      path: `/admin/api/auth/session`,
      method: "GET",
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
    id: string,
    data: AddAliasRequest,
    params: RequestParams = {},
  ) =>
    this.http.request<StatusOnlyResponse>({
      operationId: "holoAddAlias",
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
   * @name HoloAddMember
   * @request POST:/admin/api/holo/members
   */
  holoAddMember = (data: AddMemberRequest, params: RequestParams = {}) =>
    this.http.request<StatusOnlyResponse>({
      operationId: "holoAddMember",
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
   * @name HoloAddRoom
   * @request POST:/admin/api/holo/rooms
   */
  holoAddRoom = (data: AddRoomRequest, params: RequestParams = {}) =>
    this.http.request<StatusOnlyResponse>({
      operationId: "holoAddRoom",
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
   * @name HoloDeleteAlarm
   * @request DELETE:/admin/api/holo/alarms
   */
  holoDeleteAlarm = (data: DeleteAlarmRequest, params: RequestParams = {}) =>
    this.http.request<DeleteAlarmResponse>({
      operationId: "holoDeleteAlarm",
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
   * @name HoloGetAlarms
   * @request GET:/admin/api/holo/alarms
   */
  holoGetAlarms = (params: RequestParams = {}) =>
    this.http.request<AlarmsResponse>({
      operationId: "holoGetAlarms",
      path: `/admin/api/holo/alarms`,
      method: "GET",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloGetCalendar
   * @request GET:/admin/api/holo/members/calendar
   */
  holoGetCalendar = (
    query?: {
      /**
       * @format int32
       * @min 1
       * @max 12
       */
      month?: number | null;
      /**
       * @format int32
       * @min 2000
       * @max 2100
       */
      year?: number | null;
    },
    params: RequestParams = {},
  ) =>
    this.http.request<CalendarResponse>({
      operationId: "holoGetCalendar",
      path: `/admin/api/holo/members/calendar`,
      method: "GET",
      query: query,
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
    this.http.request<StreamsResponse>({
      operationId: "holoGetLiveStreams",
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
   * @name HoloGetMembers
   * @request GET:/admin/api/holo/members
   */
  holoGetMembers = (params: RequestParams = {}) =>
    this.http.request<MembersResponse>({
      operationId: "holoGetMembers",
      path: `/admin/api/holo/members`,
      method: "GET",
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
    this.http.request<RoomsResponse>({
      operationId: "holoGetRooms",
      path: `/admin/api/holo/rooms`,
      method: "GET",
      format: "json",
      ...params,
    });
  /**
   * No description
   *
   * @tags holo
   * @name HoloGetRoomsJoined
   * @request GET:/admin/api/holo/rooms/joined
   */
  holoGetRoomsJoined = (params: RequestParams = {}) =>
    this.http.request<JoinedRoomsResponse>({
      operationId: "holoGetRoomsJoined",
      path: `/admin/api/holo/rooms/joined`,
      method: "GET",
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
    this.http.request<SettingsResponse>({
      operationId: "holoGetSettings",
      path: `/admin/api/holo/settings`,
      method: "GET",
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
    this.http.request<StatsResponse>({
      operationId: "holoGetStats",
      path: `/admin/api/holo/stats`,
      method: "GET",
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
    this.http.request<StreamsResponse>({
      operationId: "holoGetUpcomingStreams",
      path: `/admin/api/holo/streams/upcoming`,
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
    this.http.request<YouTubeCommunityShortsOpsResponse>({
      operationId: "holoGetYouTubeCommunityShortsOps",
      path: `/admin/api/holo/stats/youtube/community-shorts`,
      method: "GET",
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
    id: string,
    data: RemoveAliasRequest,
    params: RequestParams = {},
  ) =>
    this.http.request<StatusOnlyResponse>({
      operationId: "holoRemoveAlias",
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
   * @name HoloRemoveRoom
   * @request DELETE:/admin/api/holo/rooms
   */
  holoRemoveRoom = (data: RemoveRoomRequest, params: RequestParams = {}) =>
    this.http.request<StatusOnlyResponse>({
      operationId: "holoRemoveRoom",
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
    this.http.request<SetAclResponse>({
      operationId: "holoSetAcl",
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
   * @name HoloSetGraduation
   * @request PATCH:/admin/api/holo/members/{id}/graduation
   */
  holoSetGraduation = (
    id: string,
    data: SetGraduationRequest,
    params: RequestParams = {},
  ) =>
    this.http.request<StatusOnlyResponse>({
      operationId: "holoSetGraduation",
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
   * @name HoloSetRoomName
   * @request POST:/admin/api/holo/names/room
   */
  holoSetRoomName = (data: RoomNameUpdateRequest, params: RequestParams = {}) =>
    this.http.request<StatusOnlyResponse>({
      operationId: "holoSetRoomName",
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
    this.http.request<StatusOnlyResponse>({
      operationId: "holoSetUserName",
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
   * @name HoloUpdateChannel
   * @request PATCH:/admin/api/holo/members/{id}/channel
   */
  holoUpdateChannel = (
    id: string,
    data: UpdateChannelRequest,
    params: RequestParams = {},
  ) =>
    this.http.request<StatusOnlyResponse>({
      operationId: "holoUpdateChannel",
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
   * @name HoloUpdateMemberName
   * @request PATCH:/admin/api/holo/members/{id}/name
   */
  holoUpdateMemberName = (
    id: string,
    data: UpdateMemberNameRequest,
    params: RequestParams = {},
  ) =>
    this.http.request<StatusOnlyResponse>({
      operationId: "holoUpdateMemberName",
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
   * @name HoloUpdateSettings
   * @request POST:/admin/api/holo/settings
   */
  holoUpdateSettings = (data: Settings, params: RequestParams = {}) =>
    this.http.request<SettingsUpdateResponse>({
      operationId: "holoUpdateSettings",
      path: `/admin/api/holo/settings`,
      method: "POST",
      body: data,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
}
