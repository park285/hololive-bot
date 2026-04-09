import apiClient from '@/api/client'
import type {
  AddAliasRequest,
  Member,
  MembersResponse,
  RemoveAliasRequest,
  SetGraduationRequest,
  UpdateChannelRequest,
} from './types'

interface ApiResponse<T = unknown> {
  status: string
  message?: string
  error?: string
  data?: T
}

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
      request,
    )
    return response.data
  },

  removeAlias: async (memberId: number, request: RemoveAliasRequest) => {
    const response = await apiClient.delete<ApiResponse>(
      `/holo/members/${String(memberId)}/aliases`,
      { data: request },
    )
    return response.data
  },

  setGraduation: async (memberId: number, request: SetGraduationRequest) => {
    const response = await apiClient.patch<ApiResponse>(
      `/holo/members/${String(memberId)}/graduation`,
      request,
    )
    return response.data
  },

  updateChannel: async (memberId: number, request: UpdateChannelRequest) => {
    const response = await apiClient.patch<ApiResponse>(
      `/holo/members/${String(memberId)}/channel`,
      request,
    )
    return response.data
  },

  updateName: async (memberId: number, name: string) => {
    const response = await apiClient.patch<ApiResponse>(
      `/holo/members/${String(memberId)}/name`,
      { name },
    )
    return response.data
  },
}
