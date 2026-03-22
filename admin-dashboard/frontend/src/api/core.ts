/**
 * Core Admin API (자동 생성 클라이언트 래퍼)
 *
 * 이 파일은 swagger-typescript-api로 생성된 클라이언트를 래핑하여
 * 기존 코드와의 호환성을 유지합니다.
 */

import { isAxiosError } from 'axios'
import apiClient, { createApiClient } from './client'
import { Admin } from '@/api/generated/Admin'
import type {
    AggregatedStatus as GeneratedAggregatedStatus,
    Container as GeneratedContainer,
} from '@/api/generated/data-contracts'

const adminClient = new Admin()
adminClient.instance = createApiClient('')

// 기존 인터페이스 유지를 위한 타입 변환
export interface HeartbeatResponse {
    status?: string
    rotated?: boolean
    absolute_expires_at?: number
    idle_rejected?: boolean
    absolute_expired?: boolean
    error?: string
}

export interface SessionStatusResponse {
    status: string
    authenticated: boolean
    username: string
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
    created: number
    ports: GeneratedContainer['ports']
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

    getSession: async (): Promise<SessionStatusResponse> => {
        const { data } = await adminClient.handleSessionStatus()
        return {
            status: data.status,
            authenticated: data.authenticated,
            username: data.username,
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
        const { data } = await adminClient.handleDockerHealth()
        return {
            status: data.status,
            available: data.available,
        }
    },

    getContainers: async (): Promise<DockerContainersResponse> => {
        const { data } = await adminClient.handleDockerContainers()
        const containers: DockerContainer[] = data.containers.map(
            (c: GeneratedContainer) => ({
                id: c.id,
                name: c.name,
                state: c.state,
                status: c.status,
                image: c.image,
                health: c.health ?? 'none',
                managed: true,
                paused: false,
                created: c.created,
                ports: c.ports,
            }),
        )
        return { status: data.status, containers }
    },

    restartContainer: async (name: string): Promise<StatusOnlyResponse> => {
        const { data } = await adminClient.handleDockerRestart(name)
        return { status: data.status, message: data.message }
    },

    stopContainer: async (name: string): Promise<StatusOnlyResponse> => {
        const { data } = await adminClient.handleDockerStop(name)
        return { status: data.status, message: data.message }
    },

    startContainer: async (name: string): Promise<StatusOnlyResponse> => {
        const { data } = await adminClient.handleDockerStart(name)
        return { status: data.status, message: data.message }
    },
}

// Status API
export interface ServiceStatus {
    name: string
    available: boolean
    response_time_ms?: number | null
    error?: string | null
}

export type AggregatedStatus = GeneratedAggregatedStatus

export const statusApi = {
    get: async (): Promise<AggregatedStatus> => {
        const { data } = await adminClient.handleAggregatedStatus()
        return data
    },
}
