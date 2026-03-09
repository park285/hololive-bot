/**
 * Core Admin API (자동 생성 클라이언트 래퍼)
 *
 * 이 파일은 swagger-typescript-api로 생성된 클라이언트를 래핑하여
 * 기존 코드와의 호환성을 유지합니다.
 */

import { isAxiosError } from 'axios'
import apiClient from './client'
import { Docker } from '@/api/generated/Docker'
import { Logs } from '@/api/generated/Logs'
import type { InternalServerContainerInfo } from '@/api/generated/data-contracts'

// API 공통 설정
const API_CONFIG = {
    baseURL: '/admin/api',
    withCredentials: true,
}

// 싱글톤 인스턴스
const dockerClient = new Docker(API_CONFIG)
const logsClient = new Logs(API_CONFIG)

// 기존 인터페이스 유지를 위한 타입 변환
export interface HeartbeatResponse {
    status?: string
    rotated?: boolean
    absolute_expires_at?: number
    idle_rejected?: boolean
    absolute_expired?: boolean
    error?: string
}

// DockerContainer 타입 (기존 타입과 호환 유지)
export interface DockerContainer {
    id: string
    name: string
    state: string
    status: string
    // 기존 UI에서 사용하는 필드 (Backend에서 제공하지 않으면 기본값)
    image: string
    health: string
    managed: boolean
    paused: boolean
    startedAt?: string
    // 리소스 메트릭
    cpuPercent?: number
    memoryUsageMB?: number
    memoryLimitMB?: number
    memoryPercent?: number
    networkRxMB?: number
    networkTxMB?: number
    blockReadMB?: number
    blockWriteMB?: number
    goroutineCount?: number
}

export interface DockerHealthResponse {
    status: string
    available: boolean
}

export interface DockerContainersResponse {
    status: string
    containers: DockerContainer[]
}

export interface StatusOnlyResponse {
    status: string
    message?: string
}

export interface SystemLogsResponse {
    status: string
    file: string
    lines: string[]
    count: number
    error?: string
}

export interface SystemLogFile {
    key: string
    name: string
    description: string
    exists: boolean
}

export interface SystemLogFilesResponse {
    status: string
    files: SystemLogFile[]
}

interface AuthStatusResponse {
    status?: string
    message?: string
}

// Auth API: 기존 인터페이스 유지
export const authApi = {
    login: async (username: string, password: string): Promise<void> => {
        const response = await apiClient.post<AuthStatusResponse>('/auth/login', { username, password })
        if (response.data.status !== 'ok') {
            throw new Error(response.data.message || 'Authentication failed')
        }
    },

    logout: async (): Promise<StatusOnlyResponse> => {
        const response = await apiClient.post<AuthStatusResponse>('/auth/logout')
        return {
            status: response.data.status ?? 'ok',
            message: response.data.message,
        }
    },

    heartbeat: async (idle = false, signal?: AbortSignal): Promise<HeartbeatResponse> => {
        try {
            const response = await apiClient.post('/auth/heartbeat', { idle }, { signal })
            return response.data as HeartbeatResponse
        } catch (error) {
            if (isAxiosError(error) && error.response?.data) {
                return error.response.data as HeartbeatResponse
            }
            throw error
        }
    },
}

// Docker API
export const dockerApi = {
    checkHealth: async (): Promise<DockerHealthResponse> => {
        const response = await dockerClient.healthList()
        return {
            status: response.data.status ?? 'ok',
            available: response.data.available ?? false,
        }
    },

    getContainers: async (): Promise<DockerContainersResponse> => {
        const response = await dockerClient.containersList()
        const containers: DockerContainer[] = (response.data.containers ?? []).map(
            (c: InternalServerContainerInfo) => ({
                id: c.id ?? '',
                name: c.name ?? '',
                state: c.state ?? '',
                status: c.status ?? '',
                image: '',
                health: c.state === 'running' ? 'healthy' : 'unhealthy',
                managed: true,
                paused: false,
                cpuPercent: c.cpuPercent,
                memoryUsageMB: c.memoryUsageMB,
                memoryLimitMB: c.memoryLimitMB,
                memoryPercent: c.memoryPercent,
                networkRxMB: c.networkRxMB,
                networkTxMB: c.networkTxMB,
                blockReadMB: c.blockReadMB,
                blockWriteMB: c.blockWriteMB,
                goroutineCount: c.goroutineCount,
            }),
        )
        return { status: response.data.status ?? 'ok', containers }
    },

    restartContainer: async (name: string): Promise<StatusOnlyResponse> => {
        const response = await dockerClient.containersRestartCreate(name)
        return { status: response.data.status ?? 'ok', message: response.data.message }
    },

    stopContainer: async (name: string): Promise<StatusOnlyResponse> => {
        const response = await dockerClient.containersStopCreate(name)
        return { status: response.data.status ?? 'ok', message: response.data.message }
    },

    startContainer: async (name: string): Promise<StatusOnlyResponse> => {
        const response = await dockerClient.containersStartCreate(name)
        return { status: response.data.status ?? 'ok', message: response.data.message }
    },
}

// System Logs API (Core)
export const systemLogsApi = {
    getSystemLogs: async (file = 'combined', lines = 200): Promise<SystemLogsResponse> => {
        const response = await logsClient.logsList({ file, lines })
        return {
            status: response.data.status ?? 'ok',
            file: response.data.file ?? file,
            lines: response.data.lines ?? [],
            count: response.data.count ?? 0,
            error: undefined,
        }
    },

    getSystemLogFiles: async (): Promise<SystemLogFilesResponse> => {
        const response = await logsClient.filesList()
        return {
            status: response.data.status ?? 'ok',
            files: (response.data.files ?? []).map((f) => ({
                key: f.key ?? '',
                name: f.name ?? '',
                description: f.description ?? '',
                exists: true,
            })),
        }
    },
}

// Status API
export interface ServiceStatus {
    name: string
    available: boolean
    version?: string
    uptime?: string
    goroutines: number
}

export interface AggregatedStatus {
    version: string
    uptime: string
    startedAt: number
    services: ServiceStatus[]
    totalGoroutines: number
    availableServices: number
    totalServices: number
    adminGoroutines: number
}

export const statusApi = {
    get: async (): Promise<AggregatedStatus> => {
        const response = await apiClient.get<AggregatedStatus>('/status')
        return response.data
    },
}
