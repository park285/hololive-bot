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
  InternalServerLogFilesResponse,
  InternalServerSystemLogsResponse,
} from "./data-contracts";
import { ContentType, HttpClient, RequestParams } from "./http-client";

export class Logs<
  SecurityDataType = unknown,
> extends HttpClient<SecurityDataType> {
  /**
   * @description Read last N lines from a system log file
   *
   * @tags logs
   * @name LogsList
   * @summary Get system logs
   * @request GET:/logs
   * @secure
   */
  logsList = (
    query?: {
      /**
       * Log file key
       * @default "combined"
       */
      file?: string;
      /**
       * Number of lines to fetch
       * @max 1000
       * @default 200
       */
      lines?: number;
    },
    params: RequestParams = {},
  ) =>
    this.request<InternalServerSystemLogsResponse, InternalServerErrorResponse>(
      {
        path: `/logs`,
        method: "GET",
        query: query,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      },
    );
  /**
   * @description Get available system log files
   *
   * @tags logs
   * @name FilesList
   * @summary List log files
   * @request GET:/logs/files
   * @secure
   */
  filesList = (params: RequestParams = {}) =>
    this.request<InternalServerLogFilesResponse, any>({
      path: `/logs/files`,
      method: "GET",
      secure: true,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
}
