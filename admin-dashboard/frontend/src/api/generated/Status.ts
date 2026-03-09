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

import { GithubComPark285LlmKakaoBotsAdminDashboardInternalStatusAggregatedStatus } from "./data-contracts";
import { ContentType, HttpClient, RequestParams } from "./http-client";

export class Status<
  SecurityDataType = unknown,
> extends HttpClient<SecurityDataType> {
  /**
   * @description 모든 서비스(Admin, Holo Bot, Game Bots, LLM Server)의 상태를 집계하여 반환
   *
   * @tags status
   * @name StatusList
   * @summary 통합 시스템 상태
   * @request GET:/status
   * @secure
   */
  statusList = (params: RequestParams = {}) =>
    this.request<
      GithubComPark285LlmKakaoBotsAdminDashboardInternalStatusAggregatedStatus,
      any
    >({
      path: `/status`,
      method: "GET",
      secure: true,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
}
