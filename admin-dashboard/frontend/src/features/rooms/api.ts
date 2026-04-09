import apiClient from '@/api/client'
import type { AddRoomRequest, RemoveRoomRequest, RoomsResponse } from './types'

interface ApiResponse<T = unknown> {
  status: string
  message?: string
  error?: string
  data?: T
}

interface SetACLRequest {
  enabled?: boolean
  mode?: 'whitelist' | 'blacklist'
}

interface SetACLResponse extends ApiResponse {
  enabled: boolean
  mode: string
}

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

  setACL: async (params: SetACLRequest) => {
    const response = await apiClient.post<SetACLResponse>('/holo/rooms/acl', params)
    return response.data
  },
}
