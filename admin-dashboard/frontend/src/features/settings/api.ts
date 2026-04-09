import apiClient from '@/api/client'
import type { Settings, SettingsResponse } from './types'

interface ApiResponse<T = unknown> {
  status: string
  message?: string
  error?: string
  data?: T
}

export const settingsApi = {
  get: async () => {
    const response = await apiClient.get<SettingsResponse>('/holo/settings')
    return response.data
  },
  update: async (settings: Settings) => {
    const response = await apiClient.post<ApiResponse>('/holo/settings', settings)
    return response.data
  },
}
