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

export interface AggregatedStatus {
  services: ServiceStatus[];
  uptime: string;
  version: string;
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
