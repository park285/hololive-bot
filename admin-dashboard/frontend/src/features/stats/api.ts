import { holoApi } from '@/api/holo'
import type { ChannelStatsResponse, StatsResponse } from './types'

export const statsApi = {
  get: async () => holoApi.get<StatsResponse>('/stats'),
  getChannels: async () => holoApi.get<ChannelStatsResponse>('/stats/channels'),
}
