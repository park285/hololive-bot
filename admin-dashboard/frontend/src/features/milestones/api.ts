import apiClient from '@/api/client'
import type {
  GetMilestonesParams,
  MilestonesResponse,
  MilestoneStatsResponse,
  NearMilestonesResponse,
} from './types'

export type { GetMilestonesParams } from './types'

export const milestonesApi = {
  getAchieved: async (params?: GetMilestonesParams) => {
    const response = await apiClient.get<MilestonesResponse>('/holo/milestones', {
      params: {
        limit: 50,
        ...params,
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
