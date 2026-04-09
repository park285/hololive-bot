import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '@/api/queryKeys'
import { Badge } from '@/components/ui/Badge'
import { Progress } from '@/components/ui/Progress'
import { StatCard } from '@/components/ui/StatCard'
import { milestonesApi } from '@/features/milestones/api'
import { Loader2, TrendingUp, Trophy, BellOff, Award, Video } from 'lucide-react'

const MilestonesTab = () => {
    const { data: stats, isLoading: isStatsLoading } = useQuery({
        queryKey: queryKeys.milestones.stats,
        queryFn: milestonesApi.getStats,
        staleTime: 30000,
        refetchInterval: 60000,
    })

    const { data: nearData, isLoading: isNearLoading } = useQuery({
        queryKey: queryKeys.milestones.near,
        queryFn: () => milestonesApi.getNear(0.9),
        staleTime: 30000,
        refetchInterval: 60000,
    })

    const { data: achievedData, isLoading: isAchievedLoading } = useQuery({
        queryKey: queryKeys.milestones.all,
        queryFn: () => milestonesApi.getAchieved({ limit: 20 }),
        staleTime: 60000,
        refetchInterval: 120000,
    })

    const isLoading = isStatsLoading || isNearLoading || isAchievedLoading

    if (isLoading) {
        return (
            <div className="flex justify-center items-center h-64 text-slate-400">
                <div className="animate-spin mr-2">
                    <Loader2 />
                </div>
                마일스톤 데이터를 불러오는 중…
            </div>
        )
    }

    const statCards = [
        {
            label: '총 달성 기록',
            value: stats?.stats.totalAchieved ?? 0,
            variant: 'indigo' as const,
            icon: <Trophy size={24} />,
        },
        {
            label: '달성 임박',
            value: stats?.stats.totalNearMilestone ?? 0,
            variant: 'yellow' as const,
            icon: <TrendingUp size={24} />,
        },
        {
            label: '최근 달성 (30일)',
            value: stats?.stats.recentAchievements ?? 0,
            variant: 'green' as const,
            icon: <Award size={24} />,
        },
        {
            label: '아직 알림 안보냄',
            value: stats?.stats.notNotifiedCount ?? 0,
            variant: 'rose' as const,
            icon: <BellOff size={24} />,
        },
    ]

    return (
        <div className="space-y-8">
            {/* 1. Header & Intro */}
            <div>
                <div className="flex flex-col gap-2 mb-2">
                    <h2 className="text-2xl font-bold text-slate-800 tracking-tight">Milestone Tracker</h2>
                    <p className="text-slate-500">구독자 마일스톤 달성 현황 및 임박 멤버 모니터링</p>
                </div>
            </div>

            {/* 2. Stats Grid */}
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
                {statCards.map((stat) => (
                    <div key={stat.label}>
                        <StatCard
                            label={stat.label}
                            value={stat.value}
                            icon={stat.icon}
                            variant={stat.variant}
                        />
                    </div>
                ))}
            </div>

            <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
                {/* 3. Near Milestones */}
                <div className="space-y-4">
                    <div className="flex items-center justify-between pb-2 border-b border-slate-200">
                        <h3 className="text-lg font-bold text-slate-800 flex items-center gap-2">
                            <TrendingUp size={20} className="text-amber-500" />
                            {nearData?.threshold && nearData.threshold > 0 ? '달성 임박 멤버' : '달성 근접 멤버'}
                            {nearData?.threshold && nearData.threshold > 0 && (
                                <span className="ml-2 text-xs py-1 px-2 bg-amber-50 text-amber-600 rounded-full font-medium">
                                    진행률 {(nearData.threshold * 100).toFixed(0)}% 이상
                                </span>
                            )}
                        </h3>
                        <span className="text-slate-500 text-sm font-medium">{nearData?.count ?? 0}명</span>
                    </div>

                    <div className="bg-white rounded-2xl border border-slate-200 shadow-sm overflow-hidden">
                        {(nearData?.members.length ?? 0) === 0 ? (
                            <div className="text-center py-12 text-slate-500">
                                현재 달성 임박 멤버가 없습니다.
                            </div>
                        ) : (
                            <div className="divide-y divide-slate-100">
                                {nearData?.members.map((member, idx) => (
                                    <div key={member.channelId} className="p-4 hover:bg-slate-50 transition-colors">
                                        <div className="flex items-center gap-4 mb-3">
                                            <div className="w-10 h-10 shrink-0 rounded-full bg-amber-50 text-amber-600 flex items-center justify-center font-bold">
                                                #{idx + 1}
                                            </div>
                                            <div className="flex-1 min-w-0">
                                                <div className="flex justify-between items-start">
                                                    <div>
                                                        <h4 className="font-bold text-slate-800 text-lg truncate">{member.memberName}</h4>
                                                        <div className="text-sm text-slate-500 flex items-center gap-1">
                                                            <Video size={14} />
                                                            Next: {member.nextMilestone.toLocaleString()}
                                                        </div>
                                                    </div>
                                                    <div className="text-right ml-4 shrink-0">
                                                        <div className="font-mono font-bold text-amber-600 text-lg tabular-nums">
                                                            {member.progressPct.toFixed(1)}%
                                                        </div>
                                                        <div className="text-xs text-slate-400 tabular-nums">
                                                            {member.remaining.toLocaleString()}명 남음
                                                        </div>
                                                    </div>
                                                </div>
                                            </div>
                                        </div>
                                        <div className="pl-14">
                                            <Progress value={member.progressPct} className="h-2" />
                                        </div>
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>
                </div>

                {/* 4. Recently Achieved */}
                <div className="space-y-4">
                    <div className="flex items-center justify-between pb-2 border-b border-slate-200">
                        <h3 className="text-lg font-bold text-slate-800 flex items-center gap-2">
                            <Trophy size={20} className="text-indigo-500" />
                            최근 달성 기록
                        </h3>
                    </div>

                    <div className="bg-white rounded-2xl border border-slate-200 shadow-sm overflow-hidden">
                        {(achievedData?.milestones.length ?? 0) === 0 ? (
                            <div className="text-center py-12 text-slate-500">
                                최근 달성 기록이 없습니다.
                            </div>
                        ) : (
                            <div className="divide-y divide-slate-100">
                                {achievedData?.milestones.map((milestone, idx) => (
                                    <div key={`${milestone.channelId}-${String(milestone.value)}-${String(idx)}`} className="p-4 hover:bg-slate-50 transition-colors flex items-center justify-between">
                                        <div className="flex items-center gap-4">
                                            <div className="w-10 h-10 rounded-full bg-indigo-50 text-indigo-600 flex items-center justify-center font-bold">
                                                #{idx + 1}
                                            </div>
                                            <div>
                                                <div className="font-bold text-slate-800">{milestone.memberName}</div>
                                                <div className="text-sm text-slate-500">
                                                    {milestone.value.toLocaleString()} {milestone.type}
                                                </div>
                                            </div>
                                        </div>
                                        <div className="text-right">
                                            <div className="text-xs text-slate-400 mb-1">
                                                {new Date(milestone.achievedAt).toLocaleDateString()}
                                            </div>
                                            <Badge variant={milestone.notified ? "default" : "outline"} className={milestone.notified ? "bg-emerald-500 hover:bg-emerald-600" : "text-amber-500 border-amber-500"}>
                                                {milestone.notified ? "알림 완료" : "대기 중"}
                                            </Badge>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>
                </div>
            </div>
        </div>
    )
}

export default MilestonesTab
