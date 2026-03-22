/**
 * API 통합 엔트리포인트
 *
 * 구조:
 * - Core API (auth, docker, status): generated 클라이언트 기반 (core.ts)
 * - Domain API (holo/*): 수동 정의 (holo.ts)
 * - Game Bot API (twentyq/*, turtle/*): 수동 정의 (gameBots.ts)
 */

// Core API (자동 생성 기반)
export {
  authApi,
  dockerApi,
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
  settingsApi,
  namesApi,
  milestonesApi,
  type GetMilestonesParams,
} from './holo'
