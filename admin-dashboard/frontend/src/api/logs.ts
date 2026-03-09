import { systemLogsApi } from '@/api/core'
import { holoLogsApi } from '@/api/holo'

export const logsApi = {
  get: holoLogsApi.get,
  getSystemLogs: systemLogsApi.getSystemLogs,
  getSystemLogFiles: systemLogsApi.getSystemLogFiles,
}
