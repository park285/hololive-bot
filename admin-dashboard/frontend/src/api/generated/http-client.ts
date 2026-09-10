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

import type { AxiosRequestConfig, AxiosResponse } from "axios";

export type RequestParams = Pick<AxiosRequestConfig, "signal">;
export const ContentType = { Json: "application/json" } as const;
export type ContentType = (typeof ContentType)[keyof typeof ContentType];
export interface FullRequestParams extends RequestParams {
  operationId: string;
  path: string;
  method: string;
  body?: unknown;
  query?: object;
  type?: ContentType;
  format?: "json";
  secure?: boolean;
}

/** SDK는 주입한 transport만 호출하며 자체 HTTP client나 앱 상태를 만들지 않습니다. */
export interface HttpClient {
  request<T>(params: FullRequestParams): Promise<AxiosResponse<T>>;
}
