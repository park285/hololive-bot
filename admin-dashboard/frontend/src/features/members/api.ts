import { holoApi, type HoloApiResponse } from '@/api/holo'
import type {
  AddAliasRequest,
  Member,
  MembersResponse,
  RemoveAliasRequest,
  SetGraduationRequest,
  UpdateChannelRequest,
} from './types'

export const membersApi = {
  getAll: async () => holoApi.get<MembersResponse>('/members'),

  add: async (member: Partial<Member>) => holoApi.post<HoloApiResponse>('/members', member),

  addAlias: async (memberId: number, request: AddAliasRequest) =>
    holoApi.post<HoloApiResponse>(
      `/members/${String(memberId)}/aliases`,
      request,
    ),

  removeAlias: async (memberId: number, request: RemoveAliasRequest) =>
    holoApi.delete<HoloApiResponse>(
      `/members/${String(memberId)}/aliases`,
      { data: request },
    ),

  setGraduation: async (memberId: number, request: SetGraduationRequest) =>
    holoApi.patch<HoloApiResponse>(
      `/members/${String(memberId)}/graduation`,
      request,
    ),

  updateChannel: async (memberId: number, request: UpdateChannelRequest) =>
    holoApi.patch<HoloApiResponse>(
      `/members/${String(memberId)}/channel`,
      request,
    ),

  updateName: async (memberId: number, name: string) =>
    holoApi.patch<HoloApiResponse>(
      `/members/${String(memberId)}/name`,
      { name },
    ),
}
