import { holoApi, type HoloApiResponse } from '@/api/holo'
import type { Settings, SettingsResponse } from './types'

export const settingsApi = {
  get: async () => holoApi.get<SettingsResponse>('/settings'),
  update: async (settings: Settings) => holoApi.post<HoloApiResponse>('/settings', settings),
}
