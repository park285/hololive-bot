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

export interface GetMilestonesParams {
  limit?: number
  offset?: number
  channelId?: string
  memberName?: string
}
