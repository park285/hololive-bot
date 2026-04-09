import apiClient from '@/api/client'
import type { StreamOrg, StreamsResponse } from './types'

export const streamsApi = {
  getLive: async (org: StreamOrg = 'hololive') => {
    const response = await apiClient.get<StreamsResponse>('/holo/streams/live', {
      params: { org },
    })
    return {
      ...response.data,
      streams: Array.isArray(response.data.streams) ? response.data.streams : [],
    }
  },
  getUpcoming: async (org: StreamOrg = 'hololive') => {
    const response = await apiClient.get<StreamsResponse>('/holo/streams/upcoming', {
      params: { org },
    })
    return {
      ...response.data,
      streams: Array.isArray(response.data.streams) ? response.data.streams : [],
    }
  },
}
