import apiClient from '@/api/client'
import type { AlarmsResponse } from './types'

interface ApiResponse<T = unknown> {
  status: string
  message?: string
  error?: string
  data?: T
}

interface DeleteAlarmRequest {
  roomId: string
  userId: string
  channelId: string
}

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
