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
  AggregatedStatus,
  DockerActionResponse,
  DockerContainerListResponse,
  DockerHealthResponse,
  HeartbeatRequest,
  HeartbeatResponse,
  LoginRequest,
  LoginResponse,
  SessionStatusResponse,
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
