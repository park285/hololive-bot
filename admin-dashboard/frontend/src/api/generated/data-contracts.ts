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

export interface GithubComPark285LlmKakaoBotsAdminDashboardInternalStatusAggregatedStatus {
  adminGoroutines?: number;
  availableServices?: number;
  /** 서비스별 상태 */
  services?: GithubComPark285LlmKakaoBotsAdminDashboardInternalStatusServiceStatus[];
  startedAt?: number;
  /** 집계 통계 */
  totalGoroutines?: number;
  totalServices?: number;
  uptime?: string;
  /** Admin Dashboard 자체 상태 */
  version?: string;
}

export interface GithubComPark285LlmKakaoBotsAdminDashboardInternalStatusServiceStatus {
  available?: boolean;
  goroutines?: number;
  name?: string;
  uptime?: string;
  version?: string;
}

export interface InternalServerContainerInfo {
  /** @example 100 */
  blockReadMB?: number;
  /** @example 50 */
  blockWriteMB?: number;
  /** @example 1.5 */
  cpuPercent?: number;
  /** @example 25 */
  goroutineCount?: number;
  /** @example "abc123def456" */
  id?: string;
  /** @example 512 */
  memoryLimitMB?: number;
  /** @example 25.1 */
  memoryPercent?: number;
  /** @example 128.5 */
  memoryUsageMB?: number;
  /** @example "hololive-bot" */
  name?: string;
  /** @example 10.5 */
  networkRxMB?: number;
  /** @example 5.2 */
  networkTxMB?: number;
  /** @example "running" */
  state?: string;
  /** @example "Up 2 hours" */
  status?: string;
}

export interface InternalServerContainerListResponse {
  containers?: InternalServerContainerInfo[];
  /** @example "ok" */
  status?: string;
}

export interface InternalServerDockerHealthResponse {
  /** @example true */
  available?: boolean;
  /** @example "ok" */
  status?: string;
}

export interface InternalServerErrorResponse {
  /** @example "Session expired" */
  details?: string;
  /** @example "Unauthorized" */
  error?: string;
}

export interface InternalServerHeartbeatRequest {
  /** @example false */
  idle?: boolean;
}

export interface InternalServerHeartbeatResponse {
  /** @example 1704067200 */
  absolute_expires_at?: number;
  /** @example false */
  idle_rejected?: boolean;
  /** @example true */
  rotated?: boolean;
  /** @example "ok" */
  status?: string;
}

export interface InternalServerLogFile {
  /** @example "All services combined log" */
  description?: string;
  /** @example "combined" */
  key?: string;
  /** @example "combined.log" */
  name?: string;
}

export interface InternalServerLogFilesResponse {
  files?: InternalServerLogFile[];
  /** @example "ok" */
  status?: string;
}

export interface InternalServerLoginRequest {
  /** @example "password123" */
  password: string;
  /** @example "admin" */
  username: string;
}

export interface InternalServerLoginResponse {
  /** @example "Login successful" */
  message?: string;
  /** @example "ok" */
  status?: string;
}

export interface InternalServerStatusResponse {
  /** @example "Operation successful" */
  message?: string;
  /** @example "ok" */
  status?: string;
}

export interface InternalServerSystemLogsResponse {
  /** @example 100 */
  count?: number;
  /** @example "combined" */
  file?: string;
  lines?: string[];
  /** @example "ok" */
  status?: string;
}
