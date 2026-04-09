import { holoApi, type HoloApiResponse } from '@/api/holo'
import type { AlarmsResponse } from './types'

interface DeleteAlarmRequest {
  roomId: string
  userId: string
  channelId: string
}

export const alarmsApi = {
  getAll: async () => holoApi.get<AlarmsResponse>('/alarms'),

  delete: async (request: DeleteAlarmRequest) => holoApi.delete<HoloApiResponse>('/alarms', {
    data: request,
  }),
}

export const namesApi = {
  setRoomName: async (roomId: string, roomName: string) => holoApi.post<HoloApiResponse>('/names/room', {
    roomId,
    roomName,
  }),

  setUserName: async (userId: string, userName: string) => holoApi.post<HoloApiResponse>('/names/user', {
    userId,
    userName,
  }),
}
