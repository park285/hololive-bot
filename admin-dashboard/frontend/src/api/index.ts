/**
 * API 통합 엔트리포인트
 *
 * 구조:
 * - Core API (auth, docker, logs): generated 클라이언트 기반 (core.ts)
 * - Domain API (holo/*): 수동 정의 (holo.ts)
 * - Game Bot API (twentyq/*, turtle/*): 수동 정의 (gameBots.ts)
 */

// Core API (자동 생성 기반)
export {
  authApi,
  dockerApi,
  systemLogsApi,
  statusApi,
  // Types
  type AggregatedStatus,
  type ServiceStatus,
  type HeartbeatResponse,
  type DockerContainer,
} from './core'

// Holo Bot Proxy API (수동 정의)
export {
  membersApi,
  alarmsApi,
  roomsApi,
  statsApi,
  streamsApi,
  holoLogsApi,
  settingsApi,
  namesApi,
  milestonesApi,
  type GetMilestonesParams,
} from './holo'

// 하위 호환성: logsApi (기존 코드에서 logsApi.get, logsApi.getSystemLogs 모두 사용)
import { holoLogsApi } from './holo'
import { systemLogsApi } from './core'

export const logsApi = {
  // Holo 봇 활동 로그
  get: holoLogsApi.get,
  // 시스템 로그 (Core)
  getSystemLogs: systemLogsApi.getSystemLogs,
  getSystemLogFiles: systemLogsApi.getSystemLogFiles,
}
