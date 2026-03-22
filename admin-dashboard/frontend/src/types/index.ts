// API Response Types
export interface ApiResponse<T = unknown> {
  status: string
  message?: string
  error?: string
  data?: T
}

// Member Types
export interface Member {
  id: number
  channelId: string
  name: string
  aliases: {
    ko: string[]
    ja: string[]
  }
  nameJa?: string
  nameKo?: string
  isGraduated: boolean
}

export interface MembersResponse {
  status: string
  members: Member[]
}

// Alarm Types
export interface Alarm {
  roomId: string
  roomName: string
  userId: string
  userName: string
  channelId: string
  memberName: string
}

export interface AlarmsResponse {
  status: string
  alarms: Alarm[]
}

// Room Types
export type ACLMode = 'whitelist' | 'blacklist'

export interface RoomsResponse {
  status: string
  rooms: string[]
  aclEnabled: boolean
  aclMode: ACLMode
}

// Stats Types
export interface StatsResponse {
  status: string
  members: number
  alarms: number
  rooms: number
  version: string
  uptime: string
}

// Auth Types
export interface LoginCredentials {
  username: string
  password: string
}

export interface HeartbeatResponse {
  status?: string
  rotated?: boolean
  absolute_expires_at?: number
  idle_rejected?: boolean
  absolute_expired?: boolean
  error?: string
}

// Mutation Request Types
export interface AddAliasRequest {
  type: 'ko' | 'ja'
  alias: string
}

export interface RemoveAliasRequest {
  type: 'ko' | 'ja'
  alias: string
}

export interface SetGraduationRequest {
  isGraduated: boolean
}

export interface UpdateChannelRequest {
  channelId: string
}

export interface AddRoomRequest {
  room: string
}

export interface RemoveRoomRequest {
  room: string
}

export interface DeleteAlarmRequest {
  roomId: string
  userId: string
  channelId: string
}

// Stream Types
export interface Stream {
  id: string
  title: string
  status: string
  channel_name?: string
  channel_id: string
  link?: string
  thumbnail?: string
  start_scheduled?: string
  start_actual?: string
}

export type StreamOrg = 'hololive' | 'vspo' | 'stellive' | 'independents' | 'all'

export interface StreamsResponse {
  status: string
  org?: string
  streams: Stream[]
}

// Channel Stats Types
export interface ChannelStat {
  ChannelID: string
  ChannelTitle: string
  SubscriberCount: number
  VideoCount: number
  ViewCount: number
}

export interface ChannelStatsResponse {
  status: string
  stats: Record<string, ChannelStat>
}

// Log Types
export interface LogEntry {
  timestamp: string
  type: string
  summary: string
  details?: Record<string, unknown>
}

export interface LogsResponse {
  status: string
  logs: LogEntry[]
}

// Settings Types
export interface Settings {
  alarmAdvanceMinutes: number
}

export interface SettingsResponse {
  status: string
  settings: Settings
}

export interface ServiceGoroutines {
  name: string
  goroutines: number
  available: boolean
}

export interface SystemStats {
  cpuUsage: number
  memoryUsage: number
  memoryTotal: number
  memoryUsed: number
  goroutines: number
  totalGoroutines: number
  serviceGoroutines: ServiceGoroutines[]
}

// Docker Types
export interface DockerContainer {
  name: string
  id: string
  image: string
  state: string
  status: string
  health: string
  managed: boolean
  paused: boolean
  startedAt?: string
}

// Milestone Types
export interface Milestone {
  channelId: string
  memberName: string
  type: string
  value: number
  achievedAt: string
  notified: boolean
}

export interface MilestonesResponse {
  status: string
  milestones: Milestone[]
  total: number
  limit: number
  offset: number
}

export interface NearMilestone {
  channelId: string
  memberName: string
  currentSubs: number
  nextMilestone: number
  remaining: number
  progressPct: number
}

export interface NearMilestonesResponse {
  status: string
  members: NearMilestone[]
  count: number
  threshold: number
}

export interface MilestoneStats {
  totalAchieved: number
  totalNearMilestone: number
  recentAchievements: number
  notNotifiedCount: number
}

export interface MilestoneStatsResponse {
  status: string
  stats: MilestoneStats
}

// Assistant Bot Types
export interface AssistantUser {
  id: string // Kakao User ID
  internalId: string // UUID
  isConnected: boolean
  lastActivity: string
  stats: {
    memos: number
    todos: number
    reminders: number
  }
  features: {
    morningBriefing: boolean
    weatherAlerts: boolean
    reminders: boolean
  }
}

export interface AssistantUsersResponse {
  status: string
  users: AssistantUser[]
}

export interface AssistantStatsResponse {
  status: string
  totalUsers: number
  connectedUsers: number
  disconnectedUsers: number
}

// Assistant Mutation Requests
export interface ToggleFeatureRequest {
  userId: string
  feature: 'morningBriefing' | 'weatherAlerts' | 'reminders'
  enabled: boolean
}

// Assistant ACL Types
export interface RoomACL {
  roomId: string
  level: 'blocked' | 'standard' | 'full'
  note?: string
  createdAt: string
  updatedAt: string
  createdBy?: string
}

export interface UserACL {
  userId: string
  level: 'blocked' | 'standard' | 'full'
  note?: string
  createdAt: string
  updatedAt: string
  createdBy?: string
}

export interface RoomACLsResponse {
  status: string
  rooms: RoomACL[]
}

export interface UserACLsResponse {
  status: string
  users: UserACL[]
}

export interface SetRoomACLRequest {
  roomId: string
  level: 'blocked' | 'standard' | 'full'
  note?: string
}

export interface SetUserACLRequest {
  userId: string
  level: 'blocked' | 'standard' | 'full'
  note?: string
}

export interface SchedulerJob {
  name: string
  description?: string
}

export interface SchedulerJobsResponse {
  status: string
  jobs: SchedulerJob[]
}

export interface ProcessingLock {
  chatId: string
}

export interface LocksResponse {
  status: string
  locks: ProcessingLock[]
}

export interface ChatSession {
  key: string
  chatId: string
  userId: string
  messageCount: number
}

export interface SessionsResponse {
  status: string
  sessions: ChatSession[]
}

// Common API Response (단순 성공/실패 응답용)
export interface CommonResponse {
  status: string
  message?: string
  error?: string
}
