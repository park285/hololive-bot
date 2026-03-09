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
  InternalServerContainerListResponse,
  InternalServerDockerHealthResponse,
  InternalServerErrorResponse,
  InternalServerStatusResponse,
} from "./data-contracts";
import { ContentType, HttpClient, RequestParams } from "./http-client";

export class Docker<
  SecurityDataType = unknown,
> extends HttpClient<SecurityDataType> {
  /**
   * @description Get all managed Docker containers with status and resource usage
   *
   * @tags docker
   * @name ContainersList
   * @summary List containers
   * @request GET:/docker/containers
   * @secure
   */
  containersList = (params: RequestParams = {}) =>
    this.request<
      InternalServerContainerListResponse,
      InternalServerErrorResponse
    >({
      path: `/docker/containers`,
      method: "GET",
      secure: true,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * @description Restart a managed Docker container by name
   *
   * @tags docker
   * @name ContainersRestartCreate
   * @summary Restart container
   * @request POST:/docker/containers/{name}/restart
   * @secure
   */
  containersRestartCreate = (name: string, params: RequestParams = {}) =>
    this.request<InternalServerStatusResponse, InternalServerErrorResponse>({
      path: `/docker/containers/${name}/restart`,
      method: "POST",
      secure: true,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * @description Start a stopped Docker container by name
   *
   * @tags docker
   * @name ContainersStartCreate
   * @summary Start container
   * @request POST:/docker/containers/{name}/start
   * @secure
   */
  containersStartCreate = (name: string, params: RequestParams = {}) =>
    this.request<InternalServerStatusResponse, InternalServerErrorResponse>({
      path: `/docker/containers/${name}/start`,
      method: "POST",
      secure: true,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * @description Stop a managed Docker container by name
   *
   * @tags docker
   * @name ContainersStopCreate
   * @summary Stop container
   * @request POST:/docker/containers/{name}/stop
   * @secure
   */
  containersStopCreate = (name: string, params: RequestParams = {}) =>
    this.request<InternalServerStatusResponse, InternalServerErrorResponse>({
      path: `/docker/containers/${name}/stop`,
      method: "POST",
      secure: true,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
  /**
   * @description Check if Docker daemon is accessible
   *
   * @tags docker
   * @name HealthList
   * @summary Docker health check
   * @request GET:/docker/health
   * @secure
   */
  healthList = (params: RequestParams = {}) =>
    this.request<InternalServerDockerHealthResponse, any>({
      path: `/docker/health`,
      method: "GET",
      secure: true,
      type: ContentType.Json,
      format: "json",
      ...params,
    });
}
