/**
 * Holo Bot Proxy API (도메인 봇 API)
 *
 * 이 파일은 hololive-kakao-bot-go의 Admin API를 프록시하는 엔드포인트를 정의합니다.
 * admin-dashboard 백엔드가 /admin/api/holo/* 요청을 hololive bot으로 전달합니다.
 *
 * 참고: 이 API들은 외부 서비스의 Swagger spec이므로 수동 정의를 유지합니다.
 */

import apiClient from '@/api/client'
import type {
    MembersResponse,
    AlarmsResponse,
    RoomsResponse,
    StatsResponse,
    AddAliasRequest,
    RemoveAliasRequest,
    SetGraduationRequest,
    UpdateChannelRequest,
    AddRoomRequest,
    RemoveRoomRequest,
    DeleteAlarmRequest,
    ApiResponse,
    StreamsResponse,
    ChannelStatsResponse,
    LogsResponse,
    SettingsResponse,
    Settings,
    Member,
    MilestonesResponse,
    NearMilestonesResponse,
    MilestoneStatsResponse,
    StreamOrg,
} from '@/types'

// Members API
export const membersApi = {
    getAll: async () => {
        const response = await apiClient.get<MembersResponse>('/holo/members')
        return response.data
    },

    add: async (member: Partial<Member>) => {
        const response = await apiClient.post<ApiResponse>('/holo/members', member)
        return response.data
    },

    addAlias: async (memberId: number, request: AddAliasRequest) => {
        const response = await apiClient.post<ApiResponse>(
            `/holo/members/${String(memberId)}/aliases`,
            request
        )
        return response.data
    },

    removeAlias: async (memberId: number, request: RemoveAliasRequest) => {
        const response = await apiClient.delete<ApiResponse>(
            `/holo/members/${String(memberId)}/aliases`,
            { data: request }
        )
        return response.data
    },

    setGraduation: async (memberId: number, request: SetGraduationRequest) => {
        const response = await apiClient.patch<ApiResponse>(
            `/holo/members/${String(memberId)}/graduation`,
            request
        )
        return response.data
    },

    updateChannel: async (memberId: number, request: UpdateChannelRequest) => {
        const response = await apiClient.patch<ApiResponse>(
            `/holo/members/${String(memberId)}/channel`,
            request
        )
        return response.data
    },

    updateName: async (memberId: number, name: string) => {
        const response = await apiClient.patch<ApiResponse>(
            `/holo/members/${String(memberId)}/name`,
            { name }
        )
        return response.data
    },
}

// Alarms API
export const alarmsApi = {
    getAll: async () => {
        const response = await apiClient.get<AlarmsResponse>('/holo/alarms')
        return response.data
    },

    delete: async (request: DeleteAlarmRequest) => {
        const response = await apiClient.delete<ApiResponse>('/holo/alarms', {
            data: request,
        })
        return response.data
    },
}

// Rooms API
export const roomsApi = {
    getAll: async () => {
        const response = await apiClient.get<RoomsResponse>('/holo/rooms')
        return response.data
    },

    add: async (request: AddRoomRequest) => {
        const response = await apiClient.post<ApiResponse>('/holo/rooms', request)
        return response.data
    },

    remove: async (request: RemoveRoomRequest) => {
        const response = await apiClient.delete<ApiResponse>('/holo/rooms', {
            data: request,
        })
        return response.data
    },

    setACL: async (params: { enabled?: boolean; mode?: 'whitelist' | 'blacklist' }) => {
        const response = await apiClient.post<ApiResponse & { enabled: boolean; mode: string }>('/holo/rooms/acl', params)
        return response.data
    },
}

// Stats API
export const statsApi = {
    get: async () => {
        const response = await apiClient.get<StatsResponse>('/holo/stats')
        return response.data
    },
    getChannels: async () => {
        const response = await apiClient.get<ChannelStatsResponse>('/holo/stats/channels')
        return response.data
    },
}

// Streams API
export const streamsApi = {
    getLive: async (org: StreamOrg = 'hololive') => {
        const response = await apiClient.get<StreamsResponse>('/holo/streams/live', {
            params: { org },
        })
        return {
            ...response.data,
            streams: Array.isArray(response.data.streams) ? response.data.streams : [],
        }
    },
    getUpcoming: async (org: StreamOrg = 'hololive') => {
        const response = await apiClient.get<StreamsResponse>('/holo/streams/upcoming', {
            params: { org },
        })
        return {
            ...response.data,
            streams: Array.isArray(response.data.streams) ? response.data.streams : [],
        }
    }
}

// Holo Logs API (봇 활동 로그)
export const holoLogsApi = {
    get: async () => {
        const response = await apiClient.get<LogsResponse>('/holo/logs')
        return response.data
    },
}

// Settings API
export const settingsApi = {
    get: async () => {
        const response = await apiClient.get<SettingsResponse>('/holo/settings')
        return response.data
    },
    update: async (settings: Settings) => {
        const response = await apiClient.post<ApiResponse>('/holo/settings', settings)
        return response.data
    }
}

// Names API
export const namesApi = {
    setRoomName: async (roomId: string, roomName: string) => {
        const response = await apiClient.post<ApiResponse>('/holo/names/room', {
            roomId,
            roomName,
        })
        return response.data
    },

    setUserName: async (userId: string, userName: string) => {
        const response = await apiClient.post<ApiResponse>('/holo/names/user', {
            userId,
            userName,
        })
        return response.data
    },
}

// Milestones API
export interface GetMilestonesParams {
    limit?: number
    offset?: number
    channelId?: string
    memberName?: string
}

export const milestonesApi = {
    getAchieved: async (params?: GetMilestonesParams) => {
        const response = await apiClient.get<MilestonesResponse>('/holo/milestones', {
            params: {
                limit: 50,
                ...params
            },
        })
        return response.data
    },

    getNear: async (threshold = 0.9) => {
        const response = await apiClient.get<NearMilestonesResponse>('/holo/milestones/near', {
            params: { threshold },
        })
        return response.data
    },

    getStats: async () => {
        const response = await apiClient.get<MilestoneStatsResponse>('/holo/milestones/stats')
        return response.data
    },
}
