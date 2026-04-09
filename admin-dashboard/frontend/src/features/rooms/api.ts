import { holoApi, type HoloApiResponse } from '@/api/holo'
import type { AddRoomRequest, RemoveRoomRequest, RoomsResponse } from './types'

interface SetACLRequest {
  enabled?: boolean
  mode?: 'whitelist' | 'blacklist'
}

interface SetACLResponse extends HoloApiResponse {
  enabled: boolean
  mode: string
}

export const roomsApi = {
  getAll: async () => holoApi.get<RoomsResponse>('/rooms'),

  add: async (request: AddRoomRequest) => holoApi.post<HoloApiResponse>('/rooms', request),

  remove: async (request: RemoveRoomRequest) =>
    holoApi.delete<HoloApiResponse>('/rooms', {
      data: request,
    }),

  setACL: async (params: SetACLRequest) => holoApi.post<SetACLResponse>('/rooms/acl', params),
}
