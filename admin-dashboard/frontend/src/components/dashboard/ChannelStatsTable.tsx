/**
 * 채널 통계 테이블 컴포넌트
 */

import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '@/api/queryKeys'
import { Skeleton } from '@/components/ui/Skeleton'
import { statsApi } from '@/features/stats/api'

const numberFormatter = new Intl.NumberFormat('ko-KR')

export const ChannelStatsTable = () => {
    const { data: response, isLoading, isError, error } = useQuery({
        queryKey: queryKeys.stats.channels,
        queryFn: statsApi.getChannels,
        refetchInterval: 60000,
    })

    if (isLoading) {
        return (
            <div className="space-y-4" aria-busy="true" aria-label="채널 통계 로딩 중…">
                <div className="rounded-lg border border-slate-100 overflow-hidden">
                    <div className="bg-slate-50 h-10 w-full mb-1" />
                    {Array.from({ length: 5 }, (_, i) => (
                        <div key={i} className="flex gap-4 p-4 border-b border-slate-50">
                            <Skeleton className="h-4 w-8" />
                            <Skeleton className="h-4 flex-1" />
                            <Skeleton className="h-4 w-20" />
                            <Skeleton className="h-4 w-20" />
                            <Skeleton className="h-4 w-12" />
                        </div>
                    ))}
                </div>
            </div>
        )
    }

    if (isError) {
        return (
            <div role="alert" className="text-center text-rose-500 py-8 bg-rose-50 rounded-lg border border-rose-100">
                <p className="font-medium">채널 통계를 불러올 수 없습니다</p>
                <p className="text-xs text-rose-400 mt-1">
                    {error instanceof Error ? error.message : '알 수 없는 오류가 발생했습니다'}
                </p>
            </div>
        )
    }

    const stats = response?.stats ?? {}
    const sortedStats = Object.values(stats)
        .sort((a, b) => b.SubscriberCount - a.SubscriberCount)
        .slice(0, 10)

    if (sortedStats.length === 0) {
        return (
            <div className="text-center text-slate-400 py-12 bg-white rounded-lg border border-slate-100">
                표시할 채널 통계가 없습니다
            </div>
        )
    }

    return (
        <div className="overflow-x-auto rounded-lg border border-slate-100 shadow-sm">
            <table className="w-full text-sm text-left" aria-label="상위 10개 채널 통계">
                <thead className="text-xs text-slate-500 uppercase bg-slate-50 border-b border-slate-200">
                    <tr>
                        <th scope="col" className="px-4 py-3.5 font-semibold w-12 text-center">#</th>
                        <th scope="col" className="px-4 py-3.5 font-semibold">채널명</th>
                        <th scope="col" className="px-4 py-3.5 font-semibold text-right">구독자 수</th>
                        <th scope="col" className="px-4 py-3.5 font-semibold text-right">총 조회수</th>
                        <th scope="col" className="px-4 py-3.5 font-semibold text-right">동영상 수</th>
                    </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                    {sortedStats.map((stat, idx) => (
                        <tr key={stat.ChannelID} className="bg-white hover:bg-sky-50/30 transition-colors group">
                            <td className="px-4 py-4 text-slate-400 font-bold text-center group-hover:text-sky-500 transition-colors">{idx + 1}</td>
                            <td className="px-4 py-4 font-medium text-slate-900 group-hover:text-sky-700 transition-colors">{stat.ChannelTitle}</td>
                            <td className="px-4 py-4 text-right text-slate-700 font-medium tabular-nums font-mono">
                                {numberFormatter.format(stat.SubscriberCount)}
                            </td>
                            <td className="px-4 py-4 text-right text-slate-500 tabular-nums font-mono">
                                {numberFormatter.format(stat.ViewCount)}
                            </td>
                            <td className="px-4 py-4 text-right text-slate-500 tabular-nums font-mono">
                                {numberFormatter.format(stat.VideoCount)}
                            </td>
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    )
}
