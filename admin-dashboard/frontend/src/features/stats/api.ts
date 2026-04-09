import apiClient from '@/api/client'
import type { ChannelStatsResponse, StatsResponse } from './types'

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
