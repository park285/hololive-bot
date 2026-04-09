export interface StatsResponse {
  status: string
  members: number
  alarms: number
  rooms: number
  version: string
  uptime: string
}

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
