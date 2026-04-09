import { holoApi } from '@/api/holo'
import type {
  GetMilestonesParams,
  MilestonesResponse,
  MilestoneStatsResponse,
  NearMilestonesResponse,
} from './types'

export type { GetMilestonesParams } from './types'

export const milestonesApi = {
  getAchieved: async (params?: GetMilestonesParams) => holoApi.get<MilestonesResponse>('/milestones', {
    params: {
      limit: 50,
      ...params,
    },
  }),

  getNear: async (threshold = 0.9) =>
    holoApi.get<NearMilestonesResponse>('/milestones/near', {
      params: { threshold },
    }),

  getStats: async () => holoApi.get<MilestoneStatsResponse>('/milestones/stats'),
}
