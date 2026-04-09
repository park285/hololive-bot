import { holoApi } from '@/api/holo'
import type { StreamOrg, StreamsResponse } from './types'

export const streamsApi = {
  getLive: async (org: StreamOrg = 'hololive') => {
    const response = await holoApi.get<StreamsResponse>('/streams/live', {
      params: { org },
    })
    return {
      ...response,
      streams: Array.isArray(response.streams) ? response.streams : [],
    }
  },
  getUpcoming: async (org: StreamOrg = 'hololive') => {
    const response = await holoApi.get<StreamsResponse>('/streams/upcoming', {
      params: { org },
    })
    return {
      ...response,
      streams: Array.isArray(response.streams) ? response.streams : [],
    }
  },
}
