export interface Settings {
  alarmAdvanceMinutes: number
}

export interface SettingsResponse {
  status: string
  settings: Settings
}
