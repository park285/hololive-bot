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
  InternalServerErrorResponse,
  InternalServerHeartbeatRequest,
  InternalServerHeartbeatResponse,
  InternalServerLoginRequest,
  InternalServerLoginResponse,
  InternalServerStatusResponse,
} from "./data-contracts";
import { ContentType, HttpClient, RequestParams } from "./http-client";

export class Auth<
  SecurityDataType = unknown,
> extends HttpClient<SecurityDataType> {
  /**
   * @description Keep session alive and optionally rotate session token
   *
   * @tags auth
   * @name HeartbeatCreate
   * @summary Session heartbeat
   * @request POST:/auth/heartbeat
   * @secure
   */
  heartbeatCreate = (
    request: InternalServerHeartbeatRequest,
    params: RequestParams = {},
  ) =>
    this.request<InternalServerHeartbeatResponse, InternalServerErrorResponse>({
      path: `/auth/heartbeat`,
      method: "POST",
      body: request,
      secure: true,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * @description Authenticate with username and password. Returns session cookie on success.
   *
   * @tags auth
   * @name LoginCreate
   * @summary User login
   * @request POST:/auth/login
   */
  loginCreate = (
    request: InternalServerLoginRequest,
    params: RequestParams = {},
  ) =>
    this.request<InternalServerLoginResponse, InternalServerErrorResponse>({
      path: `/auth/login`,
      method: "POST",
      body: request,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * @description Invalidate session and clear cookies
   *
   * @tags auth
   * @name LogoutCreate
   * @summary User logout
   * @request POST:/auth/logout
   * @secure
   */
  logoutCreate = (params: RequestParams = {}) =>
    this.request<InternalServerStatusResponse, any>({
      path: `/auth/logout`,
      method: "POST",
      secure: true,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
}
